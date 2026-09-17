package main

import (
	"flag"
	"os"
	"reflect"
	"testing"
)

func TestSplitCSV(t *testing.T) {
	tests := map[string][]string{
		"":                         nil,
		"admin, superadmin,,staff": {"admin", "superadmin", "staff"},
		" list , view ":            {"list", "view"},
	}

	for input, want := range tests {
		if got := splitCSV(input); !reflect.DeepEqual(got, want) {
			t.Fatalf("input %q: expected %v, got %v", input, want, got)
		}
	}
}

func TestMainRendersSQLWithFlags(t *testing.T) {
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	})

	flag.CommandLine = flag.NewFlagSet("module-seed", flag.ContinueOnError)
	os.Args = []string{
		"module-seed",
		"-name=projects",
		"-display-name=Projects",
		"-path=/projects",
		"-actions=list,view",
		"-grant-roles=admin",
	}

	main()
}
