package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnitRender(t *testing.T) {
	u := fmt.Sprintf(unitFmt, "/home/x/.local/bin/anjin-intel")
	for _, want := range []string{
		"ExecStart=/home/x/.local/bin/anjin-intel run",
		"WantedBy=default.target",
		"Restart=always",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit missing %q:\n%s", want, u)
		}
	}
}
