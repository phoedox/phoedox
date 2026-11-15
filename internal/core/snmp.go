package core

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

// SNMP devices an agent polls (switches, routers, access points without an
// agent of their own) become entries; LLDP neighbours and MAC addresses
// known in the project become wired_to relations with the port.

// SNMPDevice is one polled device of an agent report.
type SNMPDevice struct {
	Host       string          `json:"host"`
	Name       string          `json:"name,omitempty"`
	SysName    string          `json:"sysName,omitempty"`
	Descr      string          `json:"descr,omitempty"`
	ObjectID   string          `json:"objectId,omitempty"`
	Location   string          `json:"location,omitempty"`
	UptimeSec  int64           `json:"uptimeSec,omitempty"`
	Services   int             `json:"services,omitempty"`
	Interfaces []SNMPInterface `json:"interfaces,omitempty"`
	Neighbors  []SNMPNeighbor  `json:"neighbors,omitempty"`
	FDB        []SNMPFDB       `json:"fdb,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// SNMPInterface is a port of a polled device.
type SNMPInterface struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	Alias     string `json:"alias,omitempty"`
	Type      int    `json:"type,omitempty"`
	SpeedMbps uint64 `json:"speedMbps,omitempty"`
	MAC       string `json:"mac,omitempty"`
	Up        bool   `json:"up"`
}

// SNMPNeighbor is an LLDP neighbour.
type SNMPNeighbor struct {
	LocalPort string `json:"localPort"`
	ChassisID string `json:"chassisId,omitempty"`
	PortID    string `json:"portId,omitempty"`
	SysName   string `json:"sysName,omitempty"`
}

// SNMPFDB is a MAC address learned on a port.
type SNMPFDB struct {
	MAC  string `json:"mac"`
	Port string `json:"port"`
	VLAN int    `json:"vlan,omitempty"`
}

// maxMACsPerPort: ports that see more addresses lead to further switches
// or access points; their MACs say nothing about direct cabling.
const maxMACsPerPort = 3

// snmpKind guesses switch or router from sysServices (layer 2 / layer 3 bits).
func snmpKind(d SNMPDevice) string {
	if d.Services&0x04 != 0 && d.Services&0x02 == 0 {
		return "router"
	}
	return "switch"
}

// bridgeSNMP turns the polled devices of a report into entries and relations.
func (s *Service) bridgeSNMP(ctx context.Context, a Actor, projectID string, devices []SNMPDevice) {
	if len(devices) > 200 {
		devices = devices[:200]
	}
	all, err := s.DB.ListEntities(ctx, projectID, "")
	if err != nil {
		return
	}
	byMAC := map[string]store.Entity{}
	byName := map[string]store.Entity{}
	for _, e := range all {
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		for _, k := range []string{"mac", "macs"} {
			if v, ok := attrs[k].(string); ok {
				for _, m := range strings.Split(v, ",") {
					if mac := normMAC(m); mac != "" {
						byMAC[mac] = e
					}
				}
			}
		}
		byName[strings.ToLower(e.Name)] = e
		byName[e.Slug] = e
		if h, ok := attrs["hostname"].(string); ok && h != "" {
			byName[strings.ToLower(h)] = e
		}
	}
	for _, d := range devices {
		if d.Error != "" {
			continue
		}
		name := firstNonEmpty(d.Name, d.SysName, d.Host)
		slug := graph.Slugify(name)
		if slug == "" {
			continue
		}
		ports, up := 0, 0
		for _, i := range d.Interfaces {
			if i.Type == 6 || i.Type == 117 { // ethernetCsmacd, gigabitEthernet
				ports++
				if i.Up {
					up++
				}
			}
		}
		attrs := map[string]any{"managed": true, "snmp_seen": time.Now().UTC().Format(time.RFC3339)}
		if ip := net.ParseIP(d.Host); ip != nil {
			attrs["ip"] = d.Host
		}
		if d.Descr != "" {
			attrs["model"] = clipRunes(strings.SplitN(d.Descr, "\n", 2)[0], 120)
		}
		if d.Location != "" {
			attrs["snmp_location"] = d.Location
		}
		if ports > 0 {
			attrs["ports"] = ports
			attrs["ports_up"] = up
		}
		for _, i := range d.Interfaces {
			if normMAC(i.MAC) != "" {
				attrs["mac"] = i.MAC
				break
			}
		}
		devSlug, err := s.agentUpsert(ctx, a, projectID, snmpKind(d), slug, name, attrs)
		if err != nil {
			continue
		}
		dev, err := s.DB.GetEntity(ctx, projectID, devSlug)
		if err != nil {
			continue
		}
		byName[strings.ToLower(name)] = dev
		if m, ok := attrs["mac"].(string); ok {
			byMAC[normMAC(m)] = dev
		}
		existing := map[string]string{} // other entity id → port of the current edge
		if edges, err := s.DB.EdgesOf(ctx, dev.ID); err == nil {
			for _, e := range edges {
				if e.Rel != "wired_to" {
					continue
				}
				var ea map[string]any
				_ = json.Unmarshal(e.Attrs, &ea)
				port, _ := ea["port"].(string)
				other := e.From
				if other == dev.ID {
					other = e.To
				}
				existing[other] = port
			}
		}
		wire := func(other store.Entity, port string, outgoing bool) {
			if other.ID == dev.ID {
				return
			}
			if p, ok := existing[other.ID]; ok && p == port {
				return
			}
			in := EdgeInput{From: other.Slug, Rel: "wired_to", To: dev.Slug, Attrs: map[string]any{"port": port, "source": "snmp"}}
			if outgoing {
				in.From, in.To = dev.Slug, other.Slug
			}
			if _, err := s.Link(ctx, a, projectID, in); err == nil {
				existing[other.ID] = port
			}
		}
		uplinks := map[string]bool{}
		for _, n := range d.Neighbors {
			uplinks[n.LocalPort] = true
			other, ok := store.Entity{}, false
			if mac := normMAC(n.ChassisID); mac != "" {
				other, ok = byMAC[mac]
			}
			if !ok && n.SysName != "" {
				other, ok = byName[strings.ToLower(n.SysName)]
				if !ok {
					other, ok = byName[graph.Slugify(n.SysName)]
				}
			}
			if ok {
				wire(other, n.LocalPort, true)
			}
		}
		perPort := map[string]int{}
		for _, f := range d.FDB {
			perPort[f.Port]++
		}
		for _, f := range d.FDB {
			if uplinks[f.Port] || perPort[f.Port] > maxMACsPerPort {
				continue
			}
			if mac := normMAC(f.MAC); mac == "" {
				continue
			} else if other, ok := byMAC[mac]; ok {
				wire(other, f.Port, false)
			}
		}
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
