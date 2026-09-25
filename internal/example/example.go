package example

// Greet returns a greeting for the given name. Returns a default greeting
// when name is empty.
func Greet(name string) string {
	if name == "" {
		return "hello, world"
	}
	return "hello, " + name
}
