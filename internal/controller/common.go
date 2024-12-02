package controller

import "github.com/ihyoudou/go-backblaze"

func StringSlicesEqual(a, b []backblaze.LifecycleRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}
