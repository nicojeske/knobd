package midi

import (
	"fmt"
	"testing"
	"time"
)

func TestParserPush(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name  string
		bytes []byte
		want  []Message
	}{
		{
			name:  "single note on",
			bytes: []byte{0x90, 0x28, 0x7F},
			want:  []Message{{Status: 0x90, Data1: 0x28, Data2: 0x7F}},
		},
		{
			name:  "single control change",
			bytes: []byte{0xB0, 0x17, 0x03},
			want:  []Message{{Status: 0xB0, Data1: 0x17, Data2: 0x03}},
		},
		{
			name:  "two-byte program change",
			bytes: []byte{0xC0, 0x05},
			want:  []Message{{Status: 0xC0, Data1: 0x05}},
		},
		{
			name: "running status: repeated CC without repeating the status byte",
			// This is the fader's fast-sweep behavior the M02 spec calls out.
			bytes: []byte{0xB0, 0x17, 0x03, 0x17, 0x41, 0x17, 0x01},
			want: []Message{
				{Status: 0xB0, Data1: 0x17, Data2: 0x03},
				{Status: 0xB0, Data1: 0x17, Data2: 0x41},
				{Status: 0xB0, Data1: 0x17, Data2: 0x01},
			},
		},
		{
			name:  "running status: a new status byte replaces the old one",
			bytes: []byte{0x90, 0x28, 0x7F, 0x29, 0x7F, 0x80, 0x28, 0x00},
			want: []Message{
				{Status: 0x90, Data1: 0x28, Data2: 0x7F},
				{Status: 0x90, Data1: 0x29, Data2: 0x7F},
				{Status: 0x80, Data1: 0x28, Data2: 0x00},
			},
		},
		{
			name: "system realtime bytes are transparent mid-message",
			// 0xFE (active sensing) lands between a status byte and its
			// data bytes and must not disturb the message being parsed.
			bytes: []byte{0x90, 0xFE, 0x28, 0x7F},
			want:  []Message{{Status: 0x90, Data1: 0x28, Data2: 0x7F}},
		},
		{
			name:  "system realtime interleaved across running-status messages",
			bytes: []byte{0xB0, 0x17, 0x03, 0xF8, 0x17, 0x01},
			want: []Message{
				{Status: 0xB0, Data1: 0x17, Data2: 0x03},
				{Status: 0xB0, Data1: 0x17, Data2: 0x01},
			},
		},
		{
			name: "system common cancels running status",
			// 0xF1 (MTC quarter frame) sits between two CC data-byte
			// pairs; the trailing pair must NOT decode as a third CC.
			bytes: []byte{0xB0, 0x17, 0x03, 0xF1, 0x00, 0x17, 0x01},
			want: []Message{
				{Status: 0xB0, Data1: 0x17, Data2: 0x03},
			},
		},
		{
			name:  "sysex payload is skipped as an opaque span",
			bytes: []byte{0xF0, 0x00, 0x20, 0x32, 0x7F, 0xF7, 0x90, 0x28, 0x7F},
			want:  []Message{{Status: 0x90, Data1: 0x28, Data2: 0x7F}},
		},
		{
			name:  "data bytes before any status byte are discarded",
			bytes: []byte{0x03, 0x7F, 0x90, 0x28, 0x7F},
			want:  []Message{{Status: 0x90, Data1: 0x28, Data2: 0x7F}},
		},
		{
			name:  "lone status byte with no data yet yields nothing",
			bytes: []byte{0x90},
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p parser
			got := p.push(tc.bytes, now)
			if len(got) != len(tc.want) {
				t.Fatalf("push() = %+v, want %+v", got, tc.want)
			}
			for i := range got {
				g, w := got[i], tc.want[i]
				if g.Status != w.Status || g.Data1 != w.Data1 || g.Data2 != w.Data2 {
					t.Errorf("message %d = %+v, want %+v", i, g, w)
				}
				if !g.Time.Equal(now) {
					t.Errorf("message %d Time = %v, want %v", i, g.Time, now)
				}
			}
		})
	}
}

func TestParserSplitAcrossReads(t *testing.T) {
	whole := []byte{
		0x90, 0x28, 0x7F, // note on
		0xB0, 0x17, 0x03, // CC
		0x17, 0x41, // running-status CC
		0xE8, 0x00, 0x60, // pitch bend
	}
	want := []Message{
		{Status: 0x90, Data1: 0x28, Data2: 0x7F},
		{Status: 0xB0, Data1: 0x17, Data2: 0x03},
		{Status: 0xB0, Data1: 0x17, Data2: 0x41},
		{Status: 0xE8, Data1: 0x00, Data2: 0x60},
	}

	for _, chunkSize := range []int{1, 2, 3, 5, 7, len(whole)} {
		t.Run(fmt.Sprintf("chunk_size_%d", chunkSize), func(t *testing.T) {
			var p parser
			var got []Message
			now := time.Now()
			for i := 0; i < len(whole); i += chunkSize {
				end := i + chunkSize
				if end > len(whole) {
					end = len(whole)
				}
				got = append(got, p.push(whole[i:end], now)...)
			}
			if len(got) != len(want) {
				t.Fatalf("chunkSize=%d: got %+v, want %+v", chunkSize, got, want)
			}
			for i := range got {
				if got[i].Status != want[i].Status || got[i].Data1 != want[i].Data1 || got[i].Data2 != want[i].Data2 {
					t.Errorf("chunkSize=%d: message %d = %+v, want %+v", chunkSize, i, got[i], want[i])
				}
			}
		})
	}
}
