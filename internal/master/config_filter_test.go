package master

import "testing"

func TestIsReservedConfigFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"worker_settings.json", true},
		{"Worker_Settings.JSON", true},
		{"worker_settings", true},
		{"my_ticket.json", false},
		{"settings.json", false},
		{"worker_settings_backup.json", false},
	}
	for _, c := range cases {
		if got := isReservedConfigFile(c.name); got != c.want {
			t.Errorf("isReservedConfigFile(%q)=%v want %v", c.name, got, c.want)
		}
	}
}
