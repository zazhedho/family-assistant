package main

import (
	"flag"
	"io"
	"os"
	"reflect"
	"strings"
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

func TestMainRendersSpaceSQLWithFlag(t *testing.T) {
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
		os.Stdout = oldStdout
	})

	flag.CommandLine = flag.NewFlagSet("module-seed", flag.ContinueOnError)
	os.Args = []string{"module-seed", "-space"}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	main()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "space_owner") {
		t.Fatalf("space seed output missing owner role: %s", output)
	}
}
