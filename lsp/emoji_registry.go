package lsp

import (
	"sync"

	"m31labs.dev/mdpp"
)

var (
	emojiShortcodesOnce sync.Once
	emojiShortcodesList []string
)

func emojiShortcodes() []string {
	emojiShortcodesOnce.Do(func() {
		emojiShortcodesList = mdpp.EmojiShortcodes()
	})
	return emojiShortcodesList
}
