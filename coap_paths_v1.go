// Copyright 2021 The Matrix.org Foundation C.I.C.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package lb

var coapv1pathMappings = map[string]string{
	"0": "/_matrix/client/versions",
	"1": "/_matrix/client/r0/login",
	"2": "/_matrix/client/r0/capabilities",
	"3": "/_matrix/client/r0/logout",
	"4": "/_matrix/client/r0/register",
	"5": "/_matrix/client/r0/user/{userId}/filter",
	"6": "/_matrix/client/r0/user/{userId}/filter/{filterId}",
	"7": "/_matrix/client/r0/sync",
	"8": "/_matrix/client/r0/rooms/{roomId}/state/{eventType}/{stateKey}",
	"9": "/_matrix/client/r0/rooms/{roomId}/send/{eventType}/{txnId}",
	"A": "/_matrix/client/r0/rooms/{roomId}/event/{eventId}",
	"B": "/_matrix/client/r0/rooms/{roomId}/state",
	"C": "/_matrix/client/r0/rooms/{roomId}/members",
	"D": "/_matrix/client/r0/rooms/{roomId}/joined_members",
	"E": "/_matrix/client/r0/rooms/{roomId}/messages",
	"F": "/_matrix/client/r0/rooms/{roomId}/redact/{eventId}/{txnId}",
	"G": "/_matrix/client/r0/createRoom",
	"H": "/_matrix/client/r0/directory/room/{roomAlias}",
	"I": "/_matrix/client/r0/joined_rooms",
	"J": "/_matrix/client/r0/rooms/{roomId}/invite",
	"K": "/_matrix/client/r0/rooms/{roomId}/join",
	"L": "/_matrix/client/r0/join/{roomIdOrAlias}",
	"M": "/_matrix/client/r0/rooms/{roomId}/leave",
	"N": "/_matrix/client/r0/rooms/{roomId}/forget",
	"O": "/_matrix/client/r0/rooms/{roomId}/kick",
	"P": "/_matrix/client/r0/rooms/{roomId}/ban",
	"Q": "/_matrix/client/r0/rooms/{roomId}/unban",
	"R": "/_matrix/client/r0/directory/list/room/{roomId}",
	"S": "/_matrix/client/r0/publicRooms",
	"T": "/_matrix/client/r0/user_directory/search",
	"U": "/_matrix/client/r0/profile/{userId}/displayname",
	"V": "/_matrix/client/r0/profile/{userId}/avatar_url",
	"W": "/_matrix/client/r0/profile/{userId}",
	"X": "/_matrix/client/r0/voip/turnServer",
	"Y": "/_matrix/client/r0/rooms/{roomId}/typing/{userId}",
	"Z": "/_matrix/client/r0/rooms/{roomId}/receipt/{receiptType}/{eventId}",
	"a": "/_matrix/client/r0/rooms/{roomId}/read_markers",
	"b": "/_matrix/client/r0/presence/{userId}/status",
	"c": "/_matrix/client/r0/sendToDevice/{eventType}/{txnId}",
	"d": "/_matrix/client/r0/devices",
	"e": "/_matrix/client/r0/devices/{deviceId}",
	"f": "/_matrix/client/r0/delete_devices",
	"g": "/_matrix/client/r0/keys/upload",
	"h": "/_matrix/client/r0/keys/query",
	"i": "/_matrix/client/r0/keys/claim",
	"j": "/_matrix/client/r0/keys/changes",
	"k": "/_matrix/client/r0/pushers",
	"l": "/_matrix/client/r0/pushers/set",
	"m": "/_matrix/client/r0/notifications",
	"n": "/_matrix/client/r0/pushrules/",
	"o": "/_matrix/client/r0/search",
	"p": "/_matrix/client/r0/user/{userId}/rooms/{roomId}/tags",
	"q": "/_matrix/client/r0/user/{userId}/rooms/{roomId}/tags/{tag}",
	"r": "/_matrix/client/r0/user/{userId}/account_data/{type}",
	"s": "/_matrix/client/r0/user/{userId}/rooms/{roomId}/account_data/{type}",
	"t": "/_matrix/client/r0/rooms/{roomId}/context/{eventId}",
	"u": "/_matrix/client/r0/rooms/{roomId}/report/{eventId}",
}
