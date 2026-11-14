package core

import (
	"context"
	"encoding/json"
	"math/big"
	"net/netip"
	"sort"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// IP address management (ADR-0047): subnets are entries of kind subnet
// (CIDR, VLAN, gateway, DHCP range); addresses come from the ip fields of
// all entries. Phoedox reports usage, free addresses and conflicts.

// IPUser is an entry that uses an address.
type IPUser struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// IPAddress is a used address of a subnet.
type IPAddress struct {
	IP      string   `json:"ip"`
	Entries []IPUser `json:"entries"`
	InDHCP  bool     `json:"inDhcp,omitempty"`
	Gateway bool     `json:"gateway,omitempty"`
}

// SubnetUsage is a subnet with its addresses.
type SubnetUsage struct {
	Slug      string      `json:"slug"`
	Name      string      `json:"name"`
	CIDR      string      `json:"cidr"`
	VLAN      int         `json:"vlan,omitempty"`
	Gateway   string      `json:"gateway,omitempty"`
	DHCP      string      `json:"dhcp,omitempty"` // "start – end"
	Size      string      `json:"size"`           // usable addresses (text: IPv6 is large)
	Used      int         `json:"used"`
	Free      string      `json:"free"`
	Percent   float64     `json:"percent"`
	NextFree  string      `json:"nextFree,omitempty"` // outside DHCP and not the gateway
	Addresses []IPAddress `json:"addresses"`
	Invalid   bool        `json:"invalid,omitempty"` // the CIDR could not be read
}

// IPAMReport is the address overview of a project.
type IPAMReport struct {
	Subnets   []SubnetUsage `json:"subnets"`
	Conflicts []IPAddress   `json:"conflicts"` // addresses used by more than one entry
	Outside   []IPAddress   `json:"outside"`   // addresses in no known subnet
}

// usable counts the host addresses of a prefix (IPv4 without network and
// broadcast address, except /31 and /32).
func usable(p netip.Prefix) *big.Int {
	bits := p.Addr().BitLen() - p.Bits()
	n := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	if p.Addr().Is4() && bits >= 2 {
		n.Sub(n, big.NewInt(2))
	}
	return n
}

func hostRange(p netip.Prefix) (first, last netip.Addr) {
	p = p.Masked()
	first = p.Addr()
	bits := p.Addr().BitLen() - p.Bits()
	last = first
	b := last.AsSlice()
	for i := 0; i < bits; i++ {
		byteIdx := len(b) - 1 - i/8
		b[byteIdx] |= 1 << (i % 8)
	}
	last, _ = netip.AddrFromSlice(b)
	if p.Addr().Is4() && bits >= 2 {
		first, last = first.Next(), last.Prev()
	}
	return first, last
}

func parseIP(v any) (netip.Addr, bool) {
	s, ok := v.(string)
	if !ok {
		return netip.Addr{}, false
	}
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	a, err := netip.ParseAddr(s)
	return a.Unmap(), err == nil
}

// IPAM returns subnets with their used and free addresses (readers).
func (s *Service) IPAM(ctx context.Context, a Actor, ref string) (IPAMReport, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return IPAMReport{}, err
	}
	m, err := s.model(ctx, p.ID)
	if err != nil {
		return IPAMReport{}, err
	}
	ipFields := map[string][]string{} // kind → attributes of type ip
	for _, k := range m.Kinds {
		for _, at := range k.Attrs {
			if at.Type == "ip" && !(k.ID == "subnet") {
				ipFields[k.ID] = append(ipFields[k.ID], at.Name)
			}
		}
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil {
		return IPAMReport{}, err
	}
	type subnet struct {
		SubnetUsage
		prefix      netip.Prefix
		gw, ds, de  netip.Addr
		hasDHCP, ok bool
	}
	var subnets []*subnet
	users := map[netip.Addr][]IPUser{}
	for _, e := range ents {
		attrs := map[string]any{}
		_ = json.Unmarshal(e.Attrs, &attrs)
		if e.Kind == "subnet" {
			sn := &subnet{SubnetUsage: SubnetUsage{Slug: e.Slug, Name: e.Name, Addresses: []IPAddress{}}}
			sn.CIDR, _ = attrs["cidr"].(string)
			sn.VLAN = int(num(attrs["vlan"]))
			if pf, err := netip.ParsePrefix(strings.TrimSpace(sn.CIDR)); err == nil {
				sn.prefix, sn.ok = pf.Masked(), true
			} else {
				sn.Invalid = true
			}
			if gw, ok := parseIP(attrs["gateway"]); ok {
				sn.gw, sn.Gateway = gw, gw.String()
			}
			ds, ok1 := parseIP(attrs["dhcp_start"])
			de, ok2 := parseIP(attrs["dhcp_end"])
			if ok1 && ok2 && ds.Compare(de) <= 0 {
				sn.ds, sn.de, sn.hasDHCP, sn.DHCP = ds, de, true, ds.String()+" – "+de.String()
			}
			subnets = append(subnets, sn)
			continue
		}
		fields := append([]string{"ip"}, ipFields[e.Kind]...)
		seen := map[netip.Addr]bool{}
		for _, f := range fields {
			if ip, ok := parseIP(attrs[f]); ok && !seen[ip] {
				seen[ip] = true
				users[ip] = append(users[ip], IPUser{Slug: e.Slug, Name: e.Name, Kind: e.Kind})
			}
		}
	}
	// Most specific subnet first.
	sort.SliceStable(subnets, func(i, j int) bool { return subnets[i].prefix.Bits() > subnets[j].prefix.Bits() })
	rep := IPAMReport{Subnets: []SubnetUsage{}, Conflicts: []IPAddress{}, Outside: []IPAddress{}}
	ips := make([]netip.Addr, 0, len(users))
	for ip := range users {
		ips = append(ips, ip)
	}
	sort.Slice(ips, func(i, j int) bool { return ips[i].Less(ips[j]) })
	for _, ip := range ips {
		us := users[ip]
		if len(us) > 1 {
			rep.Conflicts = append(rep.Conflicts, IPAddress{IP: ip.String(), Entries: us})
		}
		placed := false
		for _, sn := range subnets {
			if sn.ok && sn.prefix.Contains(ip) {
				in := sn.hasDHCP && ip.Compare(sn.ds) >= 0 && ip.Compare(sn.de) <= 0
				sn.Addresses = append(sn.Addresses, IPAddress{IP: ip.String(), Entries: us, InDHCP: in, Gateway: ip == sn.gw})
				placed = true // also counts for every larger subnet that contains it
			}
		}
		if !placed {
			rep.Outside = append(rep.Outside, IPAddress{IP: ip.String(), Entries: us})
		}
	}
	for _, sn := range subnets {
		if sn.ok {
			size := usable(sn.prefix)
			used := len(sn.Addresses)
			if sn.gw.IsValid() && sn.prefix.Contains(sn.gw) && !slicesHasIP(sn.Addresses, sn.gw) {
				used++ // the gateway is taken even without an entry
			}
			sn.Used = used
			sn.Size = size.String()
			free := new(big.Int).Sub(size, big.NewInt(int64(used)))
			if free.Sign() < 0 {
				free.SetInt64(0)
			}
			sn.Free = free.String()
			if size.Sign() > 0 && size.IsInt64() {
				sn.Percent = round2(float64(used) * 100 / float64(size.Int64()))
			}
			taken := map[netip.Addr]bool{sn.gw: true}
			for _, ad := range sn.Addresses {
				ip, _ := netip.ParseAddr(ad.IP)
				taken[ip] = true
			}
			// Smaller subnets inside this one are reserved for their own use.
			var inner []netip.Prefix
			for _, o := range subnets {
				if o != sn && o.ok && o.prefix.Bits() > sn.prefix.Bits() && sn.prefix.Contains(o.prefix.Addr()) {
					inner = append(inner, o.prefix)
				}
			}
			reserved := func(ip netip.Addr) bool {
				for _, pf := range inner {
					if pf.Contains(ip) {
						return true
					}
				}
				return false
			}
			first, last := hostRange(sn.prefix)
			for ip, n := first, 0; ip.IsValid() && ip.Compare(last) <= 0 && n < 65536; ip, n = ip.Next(), n+1 {
				if taken[ip] || reserved(ip) || (sn.hasDHCP && ip.Compare(sn.ds) >= 0 && ip.Compare(sn.de) <= 0) {
					continue
				}
				sn.NextFree = ip.String()
				break
			}
		}
		rep.Subnets = append(rep.Subnets, sn.SubnetUsage)
	}
	sort.SliceStable(rep.Subnets, func(i, j int) bool { return rep.Subnets[i].CIDR < rep.Subnets[j].CIDR })
	return rep, nil
}

func slicesHasIP(list []IPAddress, ip netip.Addr) bool {
	for _, a := range list {
		if a.IP == ip.String() {
			return true
		}
	}
	return false
}
