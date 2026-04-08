package main

import "github.com/go-chi/chi/v5"

func (h *clientUIHandler) Mount(r chi.Router) {
	// Web UI
	r.Get("/", h.handleDashboard)
	r.Get("/feed", h.handleFeed)
	r.Get("/feeds", h.handleFeedsList)
	r.Get("/profile", h.handleProfile)
	r.Post("/profile", h.handleProfileAction)
	r.Get("/profile/{feedId}", h.handleUserProfile)
	r.Get("/compose", h.handleCompose)
	r.Post("/compose", h.handleCompose)
	r.Get("/following", h.handleFollowing)
	r.Post("/following", h.handleFollowingAction)
	r.Get("/followers", h.handleFollowers)
	r.Post("/followers", h.handleFollowersAction)
	r.Get("/peers", h.handlePeers)
	r.Post("/peers/add", h.handlePeersAdd)
	r.Get("/blobs", h.handleBlobs)
	r.Post("/blobs/upload", h.handleBlobsUpload)
	r.Get("/blobs/{hash}", h.handleBlobsGet)
	r.Get("/room", h.handleRoom)
	r.Post("/room", h.handleRoom)
	r.Get("/messages", h.handleMessages)
	r.Get("/message/{feedId}/{seq}", h.handleMessageDetail)
	r.Get("/replication", h.handleReplication)
	r.Get("/events", h.handleEvents)
	r.Get("/settings", h.handleSettings)

	// JSON API
	r.Get("/api/state", h.handleAPIState)
	r.Get("/api/feed", h.handleAPIFeed)
	r.Get("/api/feed/{feedId}", h.handleAPIFeedByID)
	r.Get("/api/feeds", h.handleAPIFeeds)
	r.Get("/api/message/{feedId}/{seq}", h.handleAPIMessage)
	r.Get("/api/peers", h.handleAPIPeers)
	r.Get("/api/messages", h.handleAPIMessages)
	r.Post("/api/publish", h.handleAPIPublish)
	r.Post("/api/connect", h.handleAPIConnect)
	r.Post("/api/follow", h.handleAPIFollow)
	r.Get("/api/replication", h.handleAPIReplication)
	r.Get("/api/whoami", h.handleAPIWhoami)
	r.Get("/api/blob/{hash}", h.handleBlobsGet)
	r.Get("/api/dm/conversations", h.handleAPIConversations)
	r.Get("/api/dm/{feed}", h.handleAPIConversation)
	r.Post("/api/dm", h.handleAPISendDM)
}
