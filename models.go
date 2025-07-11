package main

import (
	"fmt"
	"math/rand"
)

var (
	errNotSupported = fmt.Errorf("not supported")
	errGameOver     = fmt.Errorf("game over")
)

type Game struct {
	Players   []Player
	Wheel     []Card
	ShredPile []Card
}

func NewGame(players int) (*Game, error) {
	if players < 2 {
		return nil, fmt.Errorf("at least two players are required")
	}

	var game Game
	for i := range players {
		game.Players = append(game.Players, Player{
			name:     fmt.Sprintf("Player %d", i+1),
			points:   0,
			position: i,
			cards:    []Card{},
		})
	}
	return &game, nil
}

type wheel struct {
	cards []Card
}

func (w *wheel) spin(player *Player) error {
	if len(w.cards) == 0 {
		return errGameOver
	}
	randIndex := rand.Intn(len(w.cards))
	selectedCard := w.cards[randIndex]
	w.cards = append(w.cards[:randIndex], w.cards[randIndex+1:]...)
	player.cards = append(player.cards, selectedCard)
	return nil
}

type Player struct {
	name     string
	points   int
	position int
	cards    []Card
}

type Card interface {
	String() string
	Flip() error
}

type CardModifier struct {
	text string
}

func (c *CardModifier) String() string {
	return c.text
}

func (c *CardModifier) Flip() string {
	return c.text
}

type CardPrompt struct {
	text string
}

func (c *CardPrompt) String() string {
	return c.text
}

func (c *CardPrompt) Flip() string {
	return c.text
}

type CardRule struct {
	front     string
	back      string
	isFlipped bool
}

func (c *CardRule) String() string {
	if c.isFlipped {
		return c.back
	}
	return c.front
}

func (c *CardRule) Flip() string {
	if c.isFlipped {
		c.isFlipped = false
		return c.front
	}
	c.isFlipped = true
	return c.back
}

// every player passes a card to the left
func left() {}

// clone copies a card and adds it to a player
func clone() {}

func shred() {}
