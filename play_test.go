package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var testWheel = wheel{
	cards: []Card{
		CardRule{},
		CardPrompt{},
		CardModifier{},
	},
}

func TestPlay(t *testing.T) {
	game, err := NewGame(2)
	require.NoError(t, err, "make new game")
	t.Log(game)

	if len(game.Players) != 2 {
		require.Equal(t, 2, len(game.Players))
	}
}
