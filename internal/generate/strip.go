package generate

import "bytes"

// StripLeadingBanner removes the contiguous block of blank lines and
// `#`-prefixed comment lines from the start of content and returns the
// remaining bytes. Use it when writing a Template=true generator
// output to an operator's disk so the dual-audience banner in the
// repo copy doesn't leak onto their working file.
//
// Only line-comment syntaxes that use `#` (TOML, nginx, shell,
// YAML-ish) are recognized; callers producing other syntaxes should
// strip their own way.
func StripLeadingBanner(content []byte) []byte {
	lines := bytes.SplitAfter(content, []byte{'\n'})
	i := 0
	for ; i < len(lines); i++ {
		trimmed := bytes.TrimLeft(lines[i], " \t")
		if len(trimmed) == 0 {
			continue
		}
		// bytes.SplitAfter keeps the trailing '\n'; a line containing
		// only "\n" is a blank separator we also want to skip.
		if len(trimmed) == 1 && trimmed[0] == '\n' {
			continue
		}
		if trimmed[0] == '#' {
			continue
		}
		break
	}
	return bytes.Join(lines[i:], nil)
}
