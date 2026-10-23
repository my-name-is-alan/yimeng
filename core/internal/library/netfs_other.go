//go:build !linux && !darwin

package library

func isNetworkFS(string) bool { return false }
