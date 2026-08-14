package handlers

import (
	"context"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
)

type teamResponse struct {
	models.Team
	MembersPopulated []PublicUser `json:"members"`
	Lead             *PublicUser  `json:"leadId,omitempty"`
}

func (a *API) populateTeam(ctx context.Context, t models.Team) teamResponse {
	users, _ := a.usersByID(ctx, uniqueIDs(t.Members))
	resp := teamResponse{Team: t, MembersPopulated: []PublicUser{}}
	for _, m := range t.Members {
		if u, ok := users[m]; ok {
			resp.MembersPopulated = append(resp.MembersPopulated, u)
		}
	}
	if t.LeadID != nil {
		if u, ok := users[*t.LeadID]; ok {
			resp.Lead = &u
		} else {
			leadUsers, _ := a.usersByID(ctx, []primitive.ObjectID{*t.LeadID})
			if u, ok := leadUsers[*t.LeadID]; ok {
				resp.Lead = &u
			}
		}
	}
	return resp
}

// GET /api/teams
func (a *API) ListTeams(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cursor, err := a.Store.Teams().Find(ctx, bson.M{})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	teams := []teamResponse{}
	for cursor.Next(ctx) {
		var t models.Team
		if err := cursor.Decode(&t); err != nil {
			continue
		}
		teams = append(teams, a.populateTeam(ctx, t))
	}
	httpx.JSON(w, http.StatusOK, teams)
}

// GET /api/teams/:id
func (a *API) GetTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	var t models.Team
	err := a.Store.Teams().FindOne(r.Context(), bson.M{"_id": id}).Decode(&t)
	if err == mongo.ErrNoDocuments {
		httpx.Error(w, http.StatusNotFound, "Team not found.")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTeam(r.Context(), t))
}

type createTeamRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Color       string   `json:"color"`
	LeadID      string   `json:"leadId"`
	MemberIDs   []string `json:"memberIds"`
}

// POST /api/teams
func (a *API) CreateTeam(w http.ResponseWriter, r *http.Request) {
	var req createTeamRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Team name is required.")
		return
	}

	requester := auth.CurrentUser(r)
	color := req.Color
	if color == "" {
		color = "#0ea5e9"
	}
	now := time.Now()
	team := models.Team{
		ID:          primitive.NewObjectID(),
		Name:        req.Name,
		Description: req.Description,
		Color:       color,
		Members:     httpx.ObjectIDs(req.MemberIDs),
		CreatedBy:   requester.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if req.LeadID != "" {
		if leadID, err := primitive.ObjectIDFromHex(req.LeadID); err == nil {
			team.LeadID = &leadID
		}
	}

	ctx := r.Context()
	if _, err := a.Store.Teams().InsertOne(ctx, team); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(team.Members) > 0 {
		_, _ = a.Store.Users().UpdateMany(ctx, bson.M{"_id": bson.M{"$in": team.Members}}, bson.M{"$addToSet": bson.M{"teams": team.ID}})
	}

	httpx.JSON(w, http.StatusCreated, a.populateTeam(ctx, team))
}

// PUT /api/teams/:id
func (a *API) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	var req map[string]interface{}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	delete(req, "id")
	delete(req, "_id")
	req["updatedAt"] = time.Now()

	_, err := a.Store.Teams().UpdateByID(r.Context(), id, bson.M{"$set": req})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var t models.Team
	if err := a.Store.Teams().FindOne(r.Context(), bson.M{"_id": id}).Decode(&t); err != nil {
		httpx.Error(w, http.StatusNotFound, "Team not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, a.populateTeam(r.Context(), t))
}

// DELETE /api/teams/:id
func (a *API) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	_, err := a.Store.Teams().DeleteOne(r.Context(), bson.M{"_id": id})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type memberRequest struct {
	UserID string `json:"userId"`
}

// POST /api/teams/:id/members
func (a *API) AddTeamMember(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	var req memberRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	userID, err := primitive.ObjectIDFromHex(req.UserID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid userId.")
		return
	}

	ctx := r.Context()
	_, err = a.Store.Teams().UpdateByID(ctx, id, bson.M{"$addToSet": bson.M{"members": userID}})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = a.Store.Users().UpdateByID(ctx, userID, bson.M{"$addToSet": bson.M{"teams": id}})

	var t models.Team
	_ = a.Store.Teams().FindOne(ctx, bson.M{"_id": id}).Decode(&t)
	httpx.JSON(w, http.StatusOK, a.populateTeam(ctx, t))
}

// DELETE /api/teams/:id/members/:userId
func (a *API) RemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	userID, ok := httpx.ObjectIDParam(w, r, "userId")
	if !ok {
		return
	}

	ctx := r.Context()
	_, err := a.Store.Teams().UpdateByID(ctx, id, bson.M{"$pull": bson.M{"members": userID}})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = a.Store.Users().UpdateByID(ctx, userID, bson.M{"$pull": bson.M{"teams": id}})

	var t models.Team
	_ = a.Store.Teams().FindOne(ctx, bson.M{"_id": id}).Decode(&t)
	httpx.JSON(w, http.StatusOK, a.populateTeam(ctx, t))
}
