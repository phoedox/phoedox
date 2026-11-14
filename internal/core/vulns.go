package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Known vulnerabilities (OSV): once a day the instance sends the package
// lists that agents reported to osv.dev and stores the affected packages on
// the host entry. Off by default, because the lists leave the instance.

const (
	settingVulnCheck = "vuln_check"
	vulnCheckEvery   = 24 * time.Hour
	osvBatch         = 1000
	maxVulnPackages  = 100
)

func (s *Service) vulnCheckOn(ctx context.Context) bool {
	v, _ := s.DB.Setting(ctx, settingVulnCheck, "false")
	return v == "true"
}

// Vuln is an affected package on a host.
type Vuln struct {
	Package string   `json:"package"`
	Version string   `json:"version"`
	IDs     []string `json:"ids"`
}

// osvEcosystem maps os-release ID/VERSION_ID to an OSV ecosystem ("" = unsupported).
func osvEcosystem(id, version string) string {
	parts := strings.Split(version, ".")
	switch id {
	case "debian":
		if parts[0] != "" {
			return "Debian:" + parts[0]
		}
	case "ubuntu":
		if len(parts) == 2 {
			eco := "Ubuntu:" + version
			if y, err := strconv.Atoi(parts[0]); err == nil && parts[1] == "04" && y%2 == 0 {
				eco += ":LTS"
			}
			return eco
		}
	case "alpine":
		if len(parts) >= 2 {
			return "Alpine:v" + parts[0] + "." + parts[1]
		}
	}
	return ""
}

type osvQuery struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Version string `json:"version"`
}

// CheckVulnerabilities runs the daily check for all tenants that enabled it.
func (s *Service) CheckVulnerabilities(ctx context.Context, now time.Time) {
	tenants, err := s.DB.Tenants(ctx)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, t := range tenants {
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		tctx := store.WithTenant(ctx, t.ID)
		if !s.vulnCheckOn(tctx) {
			continue
		}
		agents, err := s.DB.TenantAgents(tctx)
		if err != nil {
			slog.Warn("vulnerability check", "tenant", t.ID, "err", err)
			continue
		}
		for _, ag := range agents {
			if err := s.checkHostVulns(tctx, ag, now); err != nil {
				slog.Warn("vulnerability check", "agent", ag.ID, "err", err)
			}
		}
	}
}

func (s *Service) checkHostVulns(ctx context.Context, ag store.Agent, now time.Time) error {
	if ag.EntitySlug == "" || len(ag.LastReport) == 0 {
		return nil
	}
	host, err := s.DB.GetEntity(ctx, ag.ProjectID, ag.EntitySlug)
	if err != nil {
		return nil
	}
	attrs := map[string]any{}
	_ = json.Unmarshal(host.Attrs, &attrs)
	if at, ok := attrs["vulns_checked"].(string); ok {
		if t, err := time.Parse(time.RFC3339, at); err == nil && now.Sub(t) < vulnCheckEvery-time.Hour {
			return nil
		}
	}
	var r struct {
		OSID     string `json:"osId"`
		OSVer    string `json:"osVersion"`
		Packages []struct {
			Name          string `json:"name"`
			Version       string `json:"version"`
			Source        string `json:"source"`
			SourceVersion string `json:"sourceVersion"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(ag.LastReport, &r); err != nil {
		return err
	}
	eco := osvEcosystem(r.OSID, r.OSVer)
	if eco == "" || len(r.Packages) == 0 {
		return nil
	}
	// One query per source package and version (many binaries share one).
	type key struct{ name, version string }
	var keys []key
	dedupe := map[key]bool{}
	for _, p := range r.Packages {
		k := key{p.Name, p.Version}
		if p.Source != "" {
			k.name = p.Source
		}
		if p.SourceVersion != "" {
			k.version = p.SourceVersion
		}
		if !dedupe[k] {
			dedupe[k] = true
			keys = append(keys, k)
		}
	}
	var found []Vuln
	advisories := map[string]bool{}
	for start := 0; start < len(keys); start += osvBatch {
		chunk := keys[start:min(start+osvBatch, len(keys))]
		qs := make([]osvQuery, len(chunk))
		for i, k := range chunk {
			qs[i].Package.Name, qs[i].Package.Ecosystem, qs[i].Version = k.name, eco, k.version
		}
		ids, err := s.osvBatch(ctx, qs)
		if err != nil {
			return err
		}
		for i, list := range ids {
			if len(list) == 0 {
				continue
			}
			for _, id := range list {
				advisories[id] = true
			}
			if len(list) > 10 {
				list = list[:10]
			}
			found = append(found, Vuln{Package: chunk[i].name, Version: chunk[i].version, IDs: list})
		}
	}
	sort.Slice(found, func(i, j int) bool { return len(found[i].IDs) > len(found[j].IDs) })
	attrs["vulnerable_packages"] = len(found)
	attrs["vulnerabilities_count"] = len(advisories)
	if len(found) > maxVulnPackages {
		found = found[:maxVulnPackages]
	}
	attrs["vulnerabilities"] = found
	attrs["vulns_checked"] = now.UTC().Format(time.RFC3339)
	b, _ := json.Marshal(attrs)
	host.Attrs = b
	if _, _, err := s.DB.UpsertEntity(ctx, host); err != nil {
		return err
	}
	s.indexEntity(ctx, host)
	_ = s.DB.AddProjectEvent(ctx, "system", "host.vulnerabilities", host.ID, host.ProjectID, map[string]any{"packages": len(found), "advisories": len(advisories)})
	return nil
}

// osvBatch asks osv.dev; it returns the advisory ids per query.
func (s *Service) osvBatch(ctx context.Context, qs []osvQuery) ([][]string, error) {
	body, _ := json.Marshal(map[string]any{"queries": qs})
	base := s.OSVURL
	if base == "" {
		base = "https://api.osv.dev"
	}
	rctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, base+"/v1/querybatch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.RequestPolicy.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("osv.dev: %s", res.Status)
	}
	var out struct {
		Results []struct {
			Vulns []struct {
				ID string `json:"id"`
			} `json:"vulns"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([][]string, len(qs))
	for i := range out.Results {
		if i >= len(ids) {
			break
		}
		for _, v := range out.Results[i].Vulns {
			ids[i] = append(ids[i], v.ID)
		}
	}
	return ids, nil
}
