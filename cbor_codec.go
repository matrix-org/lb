package lb

import (
	"fmt"
	"io"

	cbor "github.com/fxamacker/cbor/v2"
	"github.com/matrix-org/gomatrixserverlib"
)

// CBORCodec allows the conversion between JSON and CBOR.
type CBORCodec struct {
	keys     map[string]int
	enumKeys map[int]string
	// If set:
	// - CBORToJSON emits Canonical JSON: https://matrix.org/docs/spec/appendices#canonical-json
	// - JSONToCBOR emits Canonical CBOR: RFC 7049 Section 3.9
	canonical bool
}

// NewCBORCodec creates a CBOR codec which will map the enum keys given. If canonical is set,
// the output from this codec with be in canonical format for CBOR (RFC 7049 Section 3.9) and in
// Matrix Canonical JSON for JSON (https://matrix.org/docs/spec/appendices#canonical-json). Generally,
// you don't want to set canonical to true unless you are performing tests which need to produce a
// deterministic output (e.g sorted keys).
//
// Users of this library should prefer NewCBORCodecV1 which sets up all the enum keys for you. This
// function is exposed for bleeding edge or custom enums.
func NewCBORCodec(keys map[string]int, canonical bool) (*CBORCodec, error) {
	c := &CBORCodec{
		keys:      keys,
		enumKeys:  make(map[int]string),
		canonical: canonical,
	}
	for k, v := range keys {
		if _, ok := c.enumKeys[v]; ok {
			return nil, fmt.Errorf("cbor key map: duplicate integer %d - %s", v, k)
		}
		c.enumKeys[v] = k
	}
	return c, nil
}

// CBORToJSON converts a single CBOR object into a single JSON object
func (c *CBORCodec) CBORToJSON(input io.Reader) ([]byte, error) {
	var intermediate interface{}
	if err := cbor.NewDecoder(input).Decode(&intermediate); err != nil {
		return nil, fmt.Errorf("CBORToJSON: unmarshalling cbor: %w", err)
	}
	intermediate = cborInterfaceToJSONInterface(intermediate, c.enumKeys)
	b, err := json.Marshal(intermediate)
	if err != nil {
		return nil, err
	}
	if c.canonical {
		return gomatrixserverlib.CanonicalJSON(b)
	}
	return b, nil
}

// JSONToCBOR converts a single JSON object into a single CBOR object
func (c *CBORCodec) JSONToCBOR(input io.Reader) ([]byte, error) {
	var intermediate interface{}

	if err := json.NewDecoder(input).Decode(&intermediate); err != nil {
		return nil, fmt.Errorf("JSONToCBOR: unmarshalling json: %w", err)
	}
	intermediate = jsonInterfaceToCBORInterface(intermediate, c.keys)
	if c.canonical {
		enc, err := cbor.CanonicalEncOptions().EncMode()
		if err != nil {
			return nil, fmt.Errorf("JSONToCBOR: failed to make EncMode: %w", err)
		}
		return enc.Marshal(intermediate)
	}
	return cbor.Marshal(intermediate)
}
