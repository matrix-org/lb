package lb

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// CoAPPath handles mapping to and from HTTP/CoAP paths
// The mapping function converts things like:
//   /_matrix/client/r0/sync  =>  /7
//   /_matrix/client/r0/user/@frank:localhost/account_data/im.vector.setting.breadcrumbs  =>  /r/@frank:localhost/im.vector.setting.breadcrumbs
//
// All static path segments are folded down into a single URI friendly byte, then dynamic path
// segments are overlaid in the order they appear in the the full format.
type CoAPPath struct {
	pathMappings     map[string]string
	longPathMappings map[string]string
	regexpsToCodes   map[*routeRegexp]string
}

// NewCoAPPath makes a CoAPPath with the path mappings given. `pathMappings`
// MUST be in the form:
//   {
//	    "9": "/_matrix/client/r0/rooms/{roomId}/send/{eventType}/{txnId}"
//   }
// Specifically, the keys are the path enums, and the values are the HTTP paths with `{placeholder}`
// variables. These variables are important to determine what the CoAP path output should be and MUST
// be enclosed in {} (you cannot use $).
//
// Users of this library should prefer NewCoAPPathV1 which sets up all the enum paths for you. This
// function is exposed for bleeding edge or custom enums.
func NewCoAPPath(pathMappings map[string]string) (*CoAPPath, error) {
	c := CoAPPath{
		pathMappings:     pathMappings,
		longPathMappings: make(map[string]string),
		regexpsToCodes:   make(map[*routeRegexp]string),
	}

	for k, v := range c.pathMappings {
		_, ok := c.longPathMappings[v]
		if ok {
			return nil, fmt.Errorf("longPathMapping already defined: " + v)
		}
		c.longPathMappings[v] = k

		rxp, err := newRouteRegexp(v)
		if err != nil {
			return nil, fmt.Errorf("failed to init regexp for path " + v + " : " + err.Error())
		}
		c.regexpsToCodes[rxp] = k
	}

	return &c, nil
}

// CoAPPathToHTTPPath converts a coap path to a full HTTP path e.g
// converts /7 into /_matrix/client/r0/sync
// Returns the input path if this is not a coap enum path
func (c *CoAPPath) CoAPPathToHTTPPath(p string) string {
	path := p
	if !strings.HasPrefix(p, "/") {
		path = "/" + p
	}
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return p
	}
	pattern := c.pathMappings[segments[1]]
	if pattern == "" {
		return p
	}
	if len(segments) > 2 {
		// there are user params to replace
		httpSegments := strings.Split(pattern, "/")
		coapSegIndex := 2
		for i := range httpSegments {
			if coapSegIndex >= len(segments) {
				break
			}
			if strings.HasPrefix(httpSegments[i], "{") {
				httpSegments[i] = url.PathEscape(segments[coapSegIndex])
				coapSegIndex++
			}
		}
		return strings.Join(httpSegments, "/")
	}
	return pattern
}

// HTTPPathToCoapPath converts an HTTP path into a coap path e.g
// converts /_matrix/client/r0/sync into /7
// Returns the input path if this path isn't mapped to a coap enum path
func (c *CoAPPath) HTTPPathToCoapPath(p string) string {
	path := p
	if !strings.HasPrefix(p, "/") {
		path = "/" + p
	}
	// TODO: This could be made more efficient eg prefix trees
	for r, code := range c.regexpsToCodes {
		if !r.regexp.MatchString(path) {
			continue
		}
		// extract values: the first 2 values are 0, len(path) so skip them
		var userParams []string
		matches := r.regexp.FindStringSubmatchIndex(path)
		if len(matches) > 2 {
			for i := 2; i < len(matches); i += 2 {
				val := path[matches[i]:matches[i+1]]
				userParams = append(userParams, val)
			}
		}
		paths := ""
		if len(userParams) > 0 {
			paths = "/" + strings.Join(userParams, "/")
		}
		return "/" + code + paths
	}
	return p
}

// ==================================================================
// Uses gorilla/mux regexp handling code below, modified to just keep the path handling bits
// Source: https://github.com/gorilla/mux/blob/v1.8.0/regexp.go
// ==================================================================

// routeRegexp stores a regexp to match a host or path and information to
// collect and validate route variables.
type routeRegexp struct {
	// The unmodified template.
	template string
	// Expanded regexp.
	regexp *regexp.Regexp
	// Reverse template.
	reverse string
	// Variable names.
	varsN []string
	// Variable regexps (validators).
	varsR []*regexp.Regexp
}

// newRouteRegexp parses a route template and returns a routeRegexp,
// used to match a host, a path or a query string.
//
// It will extract named variables, assemble a regexp to be matched, create
// a "reverse" template to build URLs and compile regexps to validate variable
// values used in URL building.
//
// Previously we accepted only Python-like identifiers for variable
// names ([a-zA-Z_][a-zA-Z0-9_]*), but currently the only restriction is that
// name and pattern can't be empty, and names can't contain a colon.
func newRouteRegexp(tpl string) (*routeRegexp, error) {
	// Check if it is well-formed.
	idxs, errBraces := braceIndices(tpl)
	if errBraces != nil {
		return nil, errBraces
	}
	// Backup the original.
	template := tpl
	// Now let's parse it.
	defaultPattern := "[^/]+"
	// Set a flag for strictSlash.
	endSlash := false
	if strings.HasSuffix(tpl, "/") {
		tpl = tpl[:len(tpl)-1]
		endSlash = true
	}
	varsN := make([]string, len(idxs)/2)
	varsR := make([]*regexp.Regexp, len(idxs)/2)
	pattern := bytes.NewBufferString("")
	pattern.WriteByte('^')
	reverse := bytes.NewBufferString("")
	var end int
	var err error
	for i := 0; i < len(idxs); i += 2 {
		// Set all values we are interested in.
		raw := tpl[end:idxs[i]]
		end = idxs[i+1]
		parts := strings.SplitN(tpl[idxs[i]+1:end-1], ":", 2)
		name := parts[0]
		patt := defaultPattern
		if len(parts) == 2 {
			patt = parts[1]
		}
		// Name or pattern can't be empty.
		if name == "" || patt == "" {
			return nil, fmt.Errorf("mux: missing name or pattern in %q",
				tpl[idxs[i]:end])
		}
		// Build the regexp pattern.
		fmt.Fprintf(pattern, "%s(?P<%s>%s)", regexp.QuoteMeta(raw), varGroupName(i/2), patt)

		// Build the reverse template.
		fmt.Fprintf(reverse, "%s%%s", raw)

		// Append variable name and compiled pattern.
		varsN[i/2] = name
		varsR[i/2], err = regexp.Compile(fmt.Sprintf("^%s$", patt))
		if err != nil {
			return nil, err
		}
	}
	// Add the remaining.
	raw := tpl[end:]
	pattern.WriteString(regexp.QuoteMeta(raw))
	pattern.WriteString("[/]?")
	pattern.WriteByte('$')

	reverse.WriteString(raw)
	if endSlash {
		reverse.WriteByte('/')
	}
	// Compile full regexp.
	reg, errCompile := regexp.Compile(pattern.String())
	if errCompile != nil {
		return nil, errCompile
	}

	// Check for capturing groups which used to work in older versions
	if reg.NumSubexp() != len(idxs)/2 {
		panic(fmt.Sprintf("route %s contains capture groups in its regexp. ", template) +
			"Only non-capturing groups are accepted: e.g. (?:pattern) instead of (pattern)")
	}

	// Done!
	return &routeRegexp{
		template: template,
		regexp:   reg,
		reverse:  reverse.String(),
		varsN:    varsN,
		varsR:    varsR,
	}, nil
}

// braceIndices returns the first level curly brace indices from a string.
// It returns an error in case of unbalanced braces.
func braceIndices(s string) ([]int, error) {
	var level, idx int
	var idxs []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			if level++; level == 1 {
				idx = i
			}
		case '}':
			if level--; level == 0 {
				idxs = append(idxs, idx, i+1)
			} else if level < 0 {
				return nil, fmt.Errorf("mux: unbalanced braces in %q", s)
			}
		}
	}
	if level != 0 {
		return nil, fmt.Errorf("mux: unbalanced braces in %q", s)
	}
	return idxs, nil
}

// varGroupName builds a capturing group name for the indexed variable.
func varGroupName(idx int) string {
	return "v" + strconv.Itoa(idx)
}
