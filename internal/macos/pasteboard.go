//go:build darwin

package macos

import (
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

const pasteboardTypeString = "public.utf8-plain-text"

func generalPasteboard() objc.ID {
	return class("NSPasteboard").Send(sel("generalPasteboard"))
}

// PasteboardChangeCount returns the general pasteboard's change count,
// which goes up whenever anything writes to it.
func PasteboardChangeCount() int {
	var n int
	withPool(func() { n = objc.Send[int](generalPasteboard(), sel("changeCount")) })
	return n
}

// PasteboardString returns the text on the general pasteboard, and
// whether there is any.
func PasteboardString() (string, bool) {
	var s string
	var ok bool
	withPool(func() {
		v := generalPasteboard().Send(sel("stringForType:"), nsString(pasteboardTypeString))
		if v != 0 {
			s, ok = goString(v), true
		}
	})
	return s, ok
}

// SetPasteboardString replaces what the general pasteboard holds with s.
func SetPasteboardString(s string) {
	withPool(func() {
		pb := generalPasteboard()
		objc.Send[int](pb, sel("clearContents"))
		objc.Send[bool](pb, sel("setString:forType:"), nsString(s), nsString(pasteboardTypeString))
	})
}

// PasteboardItem is one item of the pasteboard, as data by type.
type PasteboardItem struct {
	Types []string
	Data  map[string][]byte
}

// PasteboardSnapshot is everything the general pasteboard held, and its
// change count then.
type PasteboardSnapshot struct {
	Items       []PasteboardItem
	ChangeCount int
}

// SavePasteboard copies every item of the general pasteboard, in all its
// types.
func SavePasteboard() PasteboardSnapshot {
	var snap PasteboardSnapshot
	withPool(func() {
		pb := generalPasteboard()
		snap.ChangeCount = objc.Send[int](pb, sel("changeCount"))
		items := pb.Send(sel("pasteboardItems"))
		if items == 0 {
			return
		}
		n := objc.Send[uint](items, sel("count"))
		for i := uint(0); i < n; i++ {
			item := items.Send(sel("objectAtIndex:"), i)
			types := item.Send(sel("types"))
			it := PasteboardItem{Data: map[string][]byte{}}
			tn := objc.Send[uint](types, sel("count"))
			for j := uint(0); j < tn; j++ {
				t := types.Send(sel("objectAtIndex:"), j)
				name := goString(t)
				it.Types = append(it.Types, name)
				if d := item.Send(sel("dataForType:"), t); d != 0 {
					it.Data[name] = bytesOf(d)
				}
			}
			snap.Items = append(snap.Items, it)
		}
	})
	return snap
}

// RestorePasteboard puts a snapshot back on the general pasteboard.
func RestorePasteboard(snap PasteboardSnapshot) {
	withPool(func() {
		pb := generalPasteboard()
		objc.Send[int](pb, sel("clearContents"))
		for _, it := range snap.Items {
			item := class("NSPasteboardItem").Send(sel("alloc")).Send(sel("init"))
			for _, t := range it.Types {
				data, ok := it.Data[t]
				if !ok {
					continue
				}
				objc.Send[bool](item, sel("setData:forType:"), nsData(data), nsString(t))
			}
			arr := class("NSArray").Send(sel("arrayWithObject:"), item)
			objc.Send[bool](pb, sel("writeObjects:"), arr)
			item.Send(sel("release"))
		}
	})
}

func bytesOf(d objc.ID) []byte {
	n := objc.Send[uint](d, sel("length"))
	if n == 0 {
		return []byte{}
	}
	p := objc.Send[unsafe.Pointer](d, sel("bytes"))
	return append([]byte(nil), unsafe.Slice((*byte)(p), n)...)
}

func nsData(b []byte) objc.ID {
	if len(b) == 0 {
		return class("NSData").Send(sel("data"))
	}
	return class("NSData").Send(sel("dataWithBytes:length:"), unsafe.Pointer(&b[0]), uint(len(b)))
}
