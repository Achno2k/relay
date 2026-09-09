// Package slackbot is the Socket Mode bot that runs on the box. One Slack
// thread is one agent session: a mention in an unbound thread creates it,
// replies continue it, and a watcher goroutine mirrors the agent's state back
// onto the thread. Owner: session "bot".
package slackbot

import (
	"context"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
)

// Message is one Slack message, flattened to the fields the bot cares about.
type Message struct {
	TS       string
	ThreadTS string
	UserID   string
	Text     string
	BotID    string
	SubType  string
}

// FromBot reports whether the message was written by a bot rather than a
// person. The bot's own posts must never be fed back into a prompt.
func (m Message) FromBot() bool { return m.BotID != "" || m.SubType == "bot_message" }

// Button is one Block Kit button on an approval prompt.
type Button struct {
	Label    string
	ActionID string
	Value    string
	Style    string // "", "primary" or "danger"
}

// SlackClient is everything the router, prompt builder and watcher need from
// Slack. It exists so all three are testable without a network.
type SlackClient interface {
	// PostMessage posts text into a thread and returns the new message ts.
	PostMessage(ctx context.Context, channel, threadTS, text string) (string, error)
	// PostButtons posts text with a row of buttons under it.
	PostButtons(ctx context.Context, channel, threadTS, text string, buttons []Button) (string, error)
	// UpdateMessage replaces the text of an existing message and drops its buttons.
	UpdateMessage(ctx context.Context, channel, ts, text string) error
	// UploadText attaches content as a file snippet in a thread.
	UploadText(ctx context.Context, channel, threadTS, filename, title, content string) error
	AddReaction(ctx context.Context, channel, ts, name string) error
	RemoveReaction(ctx context.Context, channel, ts, name string) error
	// Thread returns every message in a thread, oldest first.
	Thread(ctx context.Context, channel, threadTS string) ([]Message, error)
	// UserName resolves a user id to a display name, falling back to the id.
	UserName(ctx context.Context, userID string) (string, error)
	// BotUserID is the id of the bot itself, used to strip mentions.
	BotUserID(ctx context.Context) (string, error)
}

// apiClient is the real SlackClient, backed by slack-go.
type apiClient struct {
	api   *slack.Client
	names map[string]string // user id -> display name, resolved once
	self  string
}

// NewSlackClient wraps a slack-go client.
func NewSlackClient(api *slack.Client) SlackClient {
	return &apiClient{api: api, names: map[string]string{}}
}

func (c *apiClient) PostMessage(ctx context.Context, channel, threadTS, text string) (string, error) {
	opts := []slack.MsgOption{slack.MsgOptionText(text, false), slack.MsgOptionDisableLinkUnfurl()}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	_, ts, err := c.api.PostMessageContext(ctx, channel, opts...)
	return ts, err
}

func (c *apiClient) PostButtons(ctx context.Context, channel, threadTS, text string, buttons []Button) (string, error) {
	elements := make([]slack.BlockElement, 0, len(buttons))
	for _, b := range buttons {
		el := slack.NewButtonBlockElement(b.ActionID, b.Value,
			slack.NewTextBlockObject(slack.PlainTextType, b.Label, false, false))
		if b.Style != "" {
			el.Style = slack.Style(b.Style)
		}
		elements = append(elements, el)
	}
	blocks := []slack.Block{
		slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil),
		slack.NewActionBlock(approvalBlockID, elements...),
	}
	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(fallbackText(text), false),
		slack.MsgOptionDisableLinkUnfurl(),
	}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	_, ts, err := c.api.PostMessageContext(ctx, channel, opts...)
	return ts, err
}

func (c *apiClient) UpdateMessage(ctx context.Context, channel, ts, text string) error {
	_, _, _, err := c.api.UpdateMessageContext(ctx, channel, ts,
		slack.MsgOptionBlocks(),
		slack.MsgOptionText(text, false))
	return err
}

func (c *apiClient) UploadText(ctx context.Context, channel, threadTS, filename, title, content string) error {
	if content == "" {
		content = "(empty)"
	}
	_, err := c.api.UploadFileV2Context(ctx, slack.UploadFileV2Parameters{
		Content:         content,
		FileSize:        len(content),
		Filename:        filename,
		Title:           title,
		Channel:         channel,
		ThreadTimestamp: threadTS,
	})
	return err
}

func (c *apiClient) AddReaction(ctx context.Context, channel, ts, name string) error {
	err := c.api.AddReactionContext(ctx, name, slack.NewRefToMessage(channel, ts))
	if err != nil && err.Error() == "already_reacted" {
		return nil
	}
	return err
}

func (c *apiClient) RemoveReaction(ctx context.Context, channel, ts, name string) error {
	err := c.api.RemoveReactionContext(ctx, name, slack.NewRefToMessage(channel, ts))
	if err != nil && err.Error() == "no_reaction" {
		return nil
	}
	return err
}

func (c *apiClient) Thread(ctx context.Context, channel, threadTS string) ([]Message, error) {
	var out []Message
	cursor := ""
	for {
		msgs, hasMore, next, err := c.api.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
			ChannelID: channel,
			Timestamp: threadTS,
			Limit:     200,
			Cursor:    cursor,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range msgs {
			out = append(out, Message{
				TS:       m.Timestamp,
				ThreadTS: m.ThreadTimestamp,
				UserID:   m.User,
				Text:     m.Text,
				BotID:    m.BotID,
				SubType:  m.SubType,
			})
		}
		if !hasMore || next == "" {
			return out, nil
		}
		cursor = next
	}
}

func (c *apiClient) UserName(ctx context.Context, userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	if n, ok := c.names[userID]; ok {
		return n, nil
	}
	u, err := c.api.GetUserInfoContext(ctx, userID)
	if err != nil {
		// A name is a nicety; never fail a prompt over it.
		c.names[userID] = userID
		return userID, nil
	}
	name := u.Profile.DisplayName
	if name == "" {
		name = u.RealName
	}
	if name == "" {
		name = u.Name
	}
	if name == "" {
		name = userID
	}
	c.names[userID] = name
	return name, nil
}

func (c *apiClient) BotUserID(ctx context.Context) (string, error) {
	if c.self != "" {
		return c.self, nil
	}
	res, err := c.api.AuthTestContext(ctx)
	if err != nil {
		return "", err
	}
	c.self = res.UserID
	return c.self, nil
}

// fallbackText is the notification preview for a blocks-only message.
func fallbackText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	if s == "" {
		s = "approval needed"
	}
	return s
}

// tsLess reports whether Slack timestamp a is older than b. Slack timestamps
// are "seconds.micros" strings; compare them numerically so a shorter string
// never sorts wrong.
func tsLess(a, b string) bool {
	fa, ea := strconv.ParseFloat(a, 64)
	fb, eb := strconv.ParseFloat(b, 64)
	if ea != nil || eb != nil {
		return a < b
	}
	return fa < fb
}
