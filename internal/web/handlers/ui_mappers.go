package handlers

import (
	"net/url"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

func mapAccountRows(accounts []db.BridgedAccountStats) []templates.AccountRow {
	rows := make([]templates.AccountRow, 0, len(accounts))
	for _, account := range accounts {
		rows = append(rows, templates.AccountRow{
			ATDID:             account.ATDID,
			SSBFeedID:         account.SSBFeedID,
			Active:            account.Active,
			TotalMessages:     account.TotalMessages,
			PublishedMessages: account.PublishedMessages,
			FailedMessages:    account.FailedMessages,
			DeferredMessages:  account.DeferredMessages,
			LastPublishedAt:   formatOptionalTime(account.LastPublishedAt),
			CreatedAt:         account.CreatedAt,
			MessagesURL:       "/messages?did=" + url.QueryEscape(account.ATDID),
		})
	}
	return rows
}
