package engine

import "time"

// Buffer is a ring of the last characters typed.
type Buffer struct {
	buf         []rune
	head, count int
}

// NewBuffer makes a buffer of capacity characters.
func NewBuffer(capacity int) *Buffer { return &Buffer{buf: make([]rune, capacity)} }

// Append adds a character, dropping the oldest when full.
func (b *Buffer) Append(r rune) {
	b.buf[(b.head+b.count)%len(b.buf)] = r
	if b.count < len(b.buf) {
		b.count++
	} else {
		b.head = (b.head + 1) % len(b.buf)
	}
}

// Clear empties the buffer.
func (b *Buffer) Clear() { b.head, b.count = 0, 0 }

// PopLast removes the last character, reporting whether there was one.
func (b *Buffer) PopLast() bool {
	if b.count == 0 {
		return false
	}
	b.count--
	return true
}

// Tail returns the last n characters, or "" when there are fewer.
func (b *Buffer) Tail(n int) string {
	if n <= 0 || n > b.count {
		return ""
	}
	out := make([]rune, n)
	start := (b.head + b.count - n) % len(b.buf)
	for i := range n {
		out[i] = b.buf[(start+i)%len(b.buf)]
	}
	return string(out)
}

// Len returns how many characters the buffer holds.
func (b *Buffer) Len() int { return b.count }

// String returns what the buffer holds.
func (b *Buffer) String() string { return b.Tail(b.count) }

// undoTimeout is how long after an expansion backspacing it all restores
// the trigger.
const undoTimeout = 3 * time.Second

// Undo tracks the last expansion, which as many backspaces as it typed
// characters undo, restoring the trigger.
type Undo struct {
	trigger    string
	replaceLen int
	at         time.Time
	has        bool
	backspaces int
	now        func() time.Time
}

// Record notes an expansion.
func (u *Undo) Record(trigger, replacement string) {
	u.trigger, u.replaceLen, u.at, u.has, u.backspaces = trigger, len([]rune(replacement)), u.clock(), true, 0
}

func (u *Undo) clock() time.Time {
	if u.now != nil {
		return u.now()
	}
	return time.Now()
}

// Backspace counts a backspace, and returns the trigger to restore once
// the whole expansion is erased.
func (u *Undo) Backspace() (trigger string, undo bool) {
	if !u.has {
		return "", false
	}
	if u.clock().Sub(u.at) > undoTimeout {
		u.Clear()
		return "", false
	}
	u.backspaces++
	if u.backspaces >= u.replaceLen {
		t := u.trigger
		u.Clear()
		return t, true
	}
	return "", false
}

// Clear forgets the expansion, as any other key does.
func (u *Undo) Clear() { u.has, u.backspaces = false, 0 }
