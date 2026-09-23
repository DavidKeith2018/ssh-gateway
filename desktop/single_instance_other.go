//go:build !windows

package main

func acquireMachineInstance() (func(), error) { return func() {}, nil }
