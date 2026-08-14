package handlers

import (
	"net/http"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
)

// GET /api/notifications
func (a *API) ListNotifications(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	ctx := r.Context()
	cursor, err := a.Store.Notifications().Find(ctx, bson.M{"user": user.ID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(100))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	notifications := []models.Notification{}
	for cursor.Next(ctx) {
		var n models.Notification
		if err := cursor.Decode(&n); err != nil {
			continue
		}
		notifications = append(notifications, n)
	}
	httpx.JSON(w, http.StatusOK, notifications)
}

// POST /api/notifications/:id/read
func (a *API) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	user := auth.CurrentUser(r)
	_, err := a.Store.Notifications().UpdateOne(r.Context(), bson.M{"_id": id, "user": user.ID}, bson.M{"$set": bson.M{"read": true}})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/notifications/read-all
func (a *API) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	_, err := a.Store.Notifications().UpdateMany(r.Context(), bson.M{"user": user.ID, "read": false}, bson.M{"$set": bson.M{"read": true}})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
