package app

// HuellaDeParaPruebas exposes huellaDe to the external test package, which
// needs the exact fingerprint of a command to build the stored event a replay
// or a mismatch expects.
func HuellaDeParaPruebas(cmd any) string { return huellaDe(cmd) }
