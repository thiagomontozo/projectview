package handlers

import (
	"net/http"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"

	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
)

// GET /api/users
func (a *API) ListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cursor, err := a.Store.Users().Find(ctx, bson.M{"active": true}, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	users := []models.User{}
	for cursor.Next(ctx) {
		var u models.User
		if err := cursor.Decode(&u); err != nil {
			continue
		}
		u.PasswordHash = ""
		users = append(users, u)
	}
	httpx.JSON(w, http.StatusOK, users)
}

// GET /api/users/:id
func (a *API) GetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	var user models.User
	err := a.Store.Users().FindOne(r.Context(), bson.M{"_id": id}).Decode(&user)
	if err == mongo.ErrNoDocuments {
		httpx.Error(w, http.StatusNotFound, "User not found.")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	user.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, user)
}

type updateUserRequest struct {
	Name          *string `json:"name"`
	Title         *string `json:"title"`
	AvatarColor   *string `json:"avatarColor"`
	NotifyByEmail *bool   `json:"notifyByEmail"`
	Role          *string `json:"role"`
	Active        *bool   `json:"active"`
}

// PUT /api/users/:id
func (a *API) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	var req updateUserRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}

	requester := auth.CurrentUser(r)
	set := bson.M{}
	if req.Name != nil {
		set["name"] = *req.Name
	}
	if req.Title != nil {
		set["title"] = *req.Title
	}
	if req.AvatarColor != nil {
		set["avatarColor"] = *req.AvatarColor
	}
	if req.NotifyByEmail != nil {
		set["notifyByEmail"] = *req.NotifyByEmail
	}
	if requester.Role == models.RoleAdmin {
		if req.Role != nil {
			set["role"] = *req.Role
		}
		if req.Active != nil {
			set["active"] = *req.Active
		}
	}

	_, err := a.Store.Users().UpdateByID(r.Context(), id, bson.M{"$set": set})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	var user models.User
	if err := a.Store.Users().FindOne(r.Context(), bson.M{"_id": id}).Decode(&user); err != nil {
		httpx.Error(w, http.StatusNotFound, "User not found.")
		return
	}
	user.PasswordHash = ""
	httpx.JSON(w, http.StatusOK, user)
}

type changePasswordRequest struct {
	Password string `json:"password"`
}

// POST /api/users/:id/password
func (a *API) ChangePassword(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.ObjectIDParam(w, r, "id")
	if !ok {
		return
	}
	var req changePasswordRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.Password) < 6 {
		httpx.Error(w, http.StatusBadRequest, "Password must be at least 6 characters.")
		return
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	_, err = a.Store.Users().UpdateByID(r.Context(), id, bson.M{"$set": bson.M{
		"passwordHash": hash, "authSource": models.AuthSourceLocal,
	}})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type workloadRow struct {
	User          models.User `json:"user"`
	OpenTasks     int64       `json:"openTasks"`
	EstimateHours float64     `json:"estimateHours"`
	Overdue       int64       `json:"overdue"`
	ProjectCount  int         `json:"projectCount"`
}

// GET /api/users/workload - resource allocation view: workload per user.
func (a *API) Workload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cursor, err := a.Store.Users().Find(ctx, bson.M{"active": true})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	rows := map[string]*workloadRow{}
	order := []string{}
	for cursor.Next(ctx) {
		var u models.User
		if err := cursor.Decode(&u); err != nil {
			continue
		}
		u.PasswordHash = ""
		rows[u.ID.Hex()] = &workloadRow{User: u}
		order = append(order, u.ID.Hex())
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"status": bson.M{"$ne": "done"}}}},
		bson.D{{Key: "$unwind", Value: "$assignees"}},
		bson.D{{Key: "$group", Value: bson.M{
			"_id":           "$assignees",
			"openTasks":     bson.M{"$sum": 1},
			"estimateHours": bson.M{"$sum": bson.M{"$ifNull": []interface{}{"$estimateHours", 0}}},
			"overdue": bson.M{"$sum": bson.M{"$cond": []interface{}{
				bson.M{"$and": []interface{}{
					bson.M{"$ne": []interface{}{"$dueDate", nil}},
					bson.M{"$lt": []interface{}{"$dueDate", "$$NOW"}},
				}}, 1, 0,
			}}},
			"projects": bson.M{"$addToSet": "$project"},
		}}},
	}

	agg, err := a.Store.Tasks().Aggregate(ctx, pipeline)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer agg.Close(ctx)

	type aggRow struct {
		ID            interface{}   `bson:"_id"`
		OpenTasks     int64         `bson:"openTasks"`
		EstimateHours float64       `bson:"estimateHours"`
		Overdue       int64         `bson:"overdue"`
		Projects      []interface{} `bson:"projects"`
	}

	for agg.Next(ctx) {
		var ar aggRow
		if err := agg.Decode(&ar); err != nil {
			continue
		}
		oid, ok := ar.ID.(interface{ Hex() string })
		if !ok {
			continue
		}
		if row, exists := rows[oid.Hex()]; exists {
			row.OpenTasks = ar.OpenTasks
			row.EstimateHours = ar.EstimateHours
			row.Overdue = ar.Overdue
			row.ProjectCount = len(ar.Projects)
		}
	}

	result := make([]*workloadRow, 0, len(order))
	for _, hex := range order {
		result = append(result, rows[hex])
	}
	httpx.JSON(w, http.StatusOK, result)
}

func hashPassword(pw string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(hash), err
}
