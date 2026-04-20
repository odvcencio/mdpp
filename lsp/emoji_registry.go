package lsp

import (
	"sync"

	"github.com/odvcencio/mdpp"
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
