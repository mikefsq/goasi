//go:build !darwin

package efw

// DebugListAll enumerates every HID device the OS can see, for diagnosing why a
// wheel isn't found. It is implemented only on darwin (IOKit); on other platforms
// it is a no-op that returns -1 so the package API stays uniform across builds.
func DebugListAll() int { return -1 }
