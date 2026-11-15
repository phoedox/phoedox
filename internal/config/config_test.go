package config

import "testing"

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		ok   bool
	}{
		{"sqlite single tenant", Config{DBDriver: DriverSQLite}, true},
		{"postgres multi tenant", Config{DBDriver: DriverPostgres, MultiTenant: true}, true},
		{"sqlite multi tenant", Config{DBDriver: DriverSQLite, MultiTenant: true}, false},
		{"unknown driver", Config{DBDriver: "mysql"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); (err == nil) != tc.ok {
				t.Fatalf("Validate() error = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("PHOEDOX_DATA_DIR", "/tmp/phx")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" || c.DBDriver != DriverSQLite || c.DBURL != "/tmp/phx/phoedox.db" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestParsePaidFeatures(t *testing.T) {
	m, err := ParsePaidFeatures(" custom_domains=2027-03-01, ai=2027-06-01 ,")
	if err != nil || len(m) != 2 || m["custom_domains"].Format("2006-01-02") != "2027-03-01" {
		t.Fatalf("%v %v", m, err)
	}
	if m, err := ParsePaidFeatures(""); err != nil || len(m) != 0 {
		t.Fatalf("empty: %v %v", m, err)
	}
	for _, bad := range []string{"custom_domains", "x=2027-03-01", "custom_domains=03/01/2027", "Bad-Name=2027-03-01"} {
		if _, err := ParsePaidFeatures(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
