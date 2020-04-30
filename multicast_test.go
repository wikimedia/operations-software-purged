// Copyright (C) 2020 Emanuele Rocca <ema@wikimedia.org>
// Copyright (C) 2020 Wikimedia Foundation, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"testing"
)

func expectErr(t *testing.T, err error) {
	if err == nil {
		t.Error("Expected err to be != nil")
	}
}

func TestMulticastExtractURL(t *testing.T) {
	// buffer nil
	_, err := extractURL(nil, 0)
	expectErr(t, err)

	buffer := make([]byte, 4096)

	// packet too short, expect error
	_, err = extractURL(buffer, 5)
	expectErr(t, err)

	// No CLR opcode, expect error
	buffer[6] = 1
	_, err = extractURL(buffer, len(buffer))
	expectErr(t, err)

	// CLR opcode set
	buffer[6] = 4
	// Method len
	buffer[15] = 4

	// URL len is 0, expect error
	_, err = extractURL(buffer, len(buffer))
	expectErr(t, err)

	expectedUrl := "https://en.wikipedia.org"

	urlLen := len(expectedUrl)
	// URL len field
	buffer[21] = byte(urlLen)

	// Copy URL to buffer
	const offset = 22
	for i := 0; i < urlLen; i++ {
		buffer[offset+i] = expectedUrl[i]
	}

	url, err := extractURL(buffer, len(buffer))
	if url != "https://en.wikipedia.org" {
		t.Fatalf("%v!=%v", url, expectedUrl)
	}

	if err != nil {
		t.Fatalf("err=%v", err)
	}
}
