//go:build !linux

package main

import "os"

// Native extended attributes and ACLs are outside the portable file writer's
// contract. Only Linux has descriptor-based attribute preservation here.
func copyBudgetExtendedAttributes(source, staged *os.File) error { return nil }
