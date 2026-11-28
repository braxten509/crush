package cmd

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/spf13/cobra"
)

func init() {
	sessionCmd.AddCommand(&cobra.Command{Use: "reference <number>", Short: "Read a numbered chat from any project", Args: cobra.ExactArgs(1), RunE: referenceChat})
}

func referenceChat(cmd *cobra.Command, args []string) error {
	number, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
	if err != nil || number < 1 {
		return fmt.Errorf("enter a positive chat number")
	}
	ctx := cmd.Context()
	path, id, err := db.ResolveChatNumber(ctx, number)
	if err != nil {
		return err
	}
	conn, err := db.ConnectReadOnly(ctx, path)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := db.New(tx)
	chat, err := queries.GetSessionByID(ctx, id)
	if err != nil {
		return err
	}
	if session.Hidden(chat.LockState) {
		return session.ErrChatLocked
	}
	rows, err := queries.ListTreeMessages(ctx, id)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Chat #%d — %s\nDatabase: %s\n", number, chat.Title, path)
	for _, row := range rows {
		if row.Role != "user" && row.Role != "assistant" {
			continue
		}
		parts, err := message.DecodeParts([]byte(row.Parts))
		if err != nil {
			return err
		}
		for _, part := range parts {
			if text, ok := part.(message.TextContent); ok && !text.Hidden && text.Text != "" {
				fmt.Fprintf(out, "\n%s:\n%s\n", row.Role, text.Text)
			}
		}
	}
	return nil
}
