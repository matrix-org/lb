package lb

import (
	"fmt"
	"io"

	cbor "github.com/fxamacker/cbor/v2"
	"github.com/matrix-org/gomatrixserverlib"
)

var (
	v1cborNumToKey = map[int]string{}
	v1cborKeyToNum = map[string]int{
		"event_id":                    1,
		"type":                        2,
		"content":                     3,
		"state_key":                   4,
		"room_id":                     5,
		"sender":                      6,
		"user_id":                     7,
		"origin_server_ts":            8,
		"unsigned":                    9,
		"prev_content":                10,
		"state":                       11,
		"timeline":                    12,
		"events":                      13,
		"limited":                     14,
		"prev_batch":                  15,
		"transaction_id":              16,
		"age":                         17,
		"redacted_because":            18,
		"next_batch":                  19,
		"presence":                    20,
		"avatar_url":                  21,
		"account_data":                22,
		"rooms":                       23,
		"join":                        24,
		"membership":                  25,
		"displayname":                 26,
		"body":                        27,
		"msgtype":                     28,
		"format":                      29,
		"formatted_body":              30,
		"ephemeral":                   31,
		"invite_state":                32,
		"leave":                       33,
		"third_party_invite":          34,
		"is_direct":                   35,
		"hashes":                      36,
		"signatures":                  37,
		"depth":                       38,
		"prev_events":                 39,
		"prev_state":                  40,
		"auth_events":                 41,
		"origin":                      42,
		"creator":                     43,
		"join_rule":                   44,
		"history_visibility":          45,
		"ban":                         46,
		"events_default":              47,
		"kick":                        48,
		"redact":                      49,
		"state_default":               50,
		"users":                       51,
		"users_default":               52,
		"reason":                      53,
		"visibility":                  54,
		"room_alias_name":             55,
		"name":                        56,
		"topic":                       57,
		"invite":                      58,
		"invite_3pid":                 59,
		"room_version":                60,
		"creation_content":            61,
		"initial_state":               62,
		"preset":                      63,
		"servers":                     64,
		"identifier":                  65,
		"user":                        66,
		"medium":                      67,
		"address":                     68,
		"password":                    69,
		"token":                       70,
		"device_id":                   71,
		"initial_device_display_name": 72,
		"access_token":                73,
		"home_server":                 74,
		"well_known":                  75,
		"base_url":                    76,
		"device_lists":                77,
		"to_device":                   78,
		"peek":                        79,
		"last_seen_ip":                80,
		"display_name":                81,
		"typing":                      82,
		"last_seen_ts":                83,
		"algorithm":                   84,
		"sender_key":                  85,
		"session_id":                  86,
		"ciphertext":                  87,
		"one_time_keys":               88,
		"timeout":                     89,
		"recent_rooms":                90,
		"chunk":                       91,
		"m.fully_read":                92,
		"device_keys":                 93,
		"failures":                    94,
		"device_display_name":         95,
		"prev_sender":                 96,
		"replaces_state":              97,
		"changed":                     98,
		"unstable_features":           99,
		"versions":                    100,
		"devices":                     101,
		"errcode":                     102,
		"error":                       103,
		"room_alias":                  104,
	}
)

type CBORCodecV1 struct {
	// If set:
	// - CBORToJSON emits Canonical JSON: https://matrix.org/docs/spec/appendices#canonical-json
	// - JSONToCBOR emits Canonical CBOR: https://www.rfc-editor.org/rfc/rfc8949#name-deterministically-encoded-c
	Canonical bool
}

func (c CBORCodecV1) CBORToJSON(input io.Reader) ([]byte, error) {
	var intermediate interface{}
	if err := cbor.NewDecoder(input).Decode(&intermediate); err != nil {
		return nil, fmt.Errorf("CBORToJSON: unmarshalling cbor: %w", err)
	}
	intermediate = cborInterfaceToJSONInterface(intermediate, v1cborNumToKey)
	b, err := json.Marshal(intermediate)
	if err != nil {
		return nil, err
	}
	if c.Canonical {
		return gomatrixserverlib.CanonicalJSON(b)
	}
	return b, nil
}

func (c CBORCodecV1) JSONToCBOR(input io.Reader) ([]byte, error) {
	var intermediate interface{}

	if err := json.NewDecoder(input).Decode(&intermediate); err != nil {
		return nil, fmt.Errorf("JSONToCBOR: unmarshalling json: %w", err)
	}
	intermediate = jsonInterfaceToCBORInterface(intermediate, v1cborKeyToNum)
	if c.Canonical {
		enc, err := cbor.CanonicalEncOptions().EncMode()
		if err != nil {
			return nil, fmt.Errorf("JSONToCBOR: failed to make EncMode: %w", err)
		}
		return enc.Marshal(intermediate)
	}
	return cbor.Marshal(intermediate)
}

func init() {
	for k, v := range v1cborKeyToNum {
		if _, ok := v1cborNumToKey[v]; ok {
			panic(fmt.Sprintf("v1 cbor key map: duplicate integer %d - %s", v, k))
		}
		v1cborNumToKey[v] = k
	}
}
