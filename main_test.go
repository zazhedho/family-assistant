package main

import "testing"

func TestFailOnErrorWithNilError(t *testing.T) {
	FailOnError(nil, "should not fail")
}
