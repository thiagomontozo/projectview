package handlers

import (
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"projectview/internal/httpx"
)

type overviewResponse struct {
	TotalProjects  int64 `json:"totalProjects"`
	ActiveProjects int64 `json:"activeProjects"`
	TotalTasks     int64 `json:"totalTasks"`
	DoneTasks      int64 `json:"doneTasks"`
	OverdueTasks   int64 `json:"overdueTasks"`
}

// GET /api/dashboard/overview
func (a *API) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var resp overviewResponse
	resp.TotalProjects, _ = a.Store.Projects().CountDocuments(ctx, bson.M{})
	resp.ActiveProjects, _ = a.Store.Projects().CountDocuments(ctx, bson.M{"status": "active"})
	resp.TotalTasks, _ = a.Store.Tasks().CountDocuments(ctx, bson.M{"parentTask": nil})
	resp.DoneTasks, _ = a.Store.Tasks().CountDocuments(ctx, bson.M{"status": "done"})
	resp.OverdueTasks, _ = a.Store.Tasks().CountDocuments(ctx, bson.M{"status": bson.M{"$ne": "done"}, "dueDate": bson.M{"$lt": time.Now()}})
	httpx.JSON(w, http.StatusOK, resp)
}

type statusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// GET /api/dashboard/status-breakdown?projectId=
func (a *API) StatusBreakdown(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	match := bson.M{"parentTask": nil}
	if pid := r.URL.Query().Get("projectId"); pid != "" {
		if id, err := primitive.ObjectIDFromHex(pid); err == nil {
			match["project"] = id
		}
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: match}},
		bson.D{{Key: "$group", Value: bson.M{"_id": "$status", "count": bson.M{"$sum": 1}}}},
	}
	cursor, err := a.Store.Tasks().Aggregate(ctx, pipeline)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	rows := []statusCount{}
	for cursor.Next(ctx) {
		var row struct {
			ID    string `bson:"_id"`
			Count int    `bson:"count"`
		}
		if err := cursor.Decode(&row); err != nil {
			continue
		}
		rows = append(rows, statusCount{Status: row.ID, Count: row.Count})
	}
	httpx.JSON(w, http.StatusOK, rows)
}

type workloadChartRow struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// GET /api/dashboard/workload-chart
func (a *API) WorkloadChart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"status": bson.M{"$ne": "done"}}}},
		bson.D{{Key: "$unwind", Value: "$assignees"}},
		bson.D{{Key: "$group", Value: bson.M{"_id": "$assignees", "count": bson.M{"$sum": 1}}}},
		bson.D{{Key: "$lookup", Value: bson.M{"from": "users", "localField": "_id", "foreignField": "_id", "as": "user"}}},
		bson.D{{Key: "$unwind", Value: "$user"}},
		bson.D{{Key: "$project", Value: bson.M{"name": "$user.name", "count": 1}}},
		bson.D{{Key: "$sort", Value: bson.M{"count": -1}}},
	}
	cursor, err := a.Store.Tasks().Aggregate(ctx, pipeline)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	rows := []workloadChartRow{}
	for cursor.Next(ctx) {
		var row workloadChartRow
		if err := cursor.Decode(&row); err != nil {
			continue
		}
		rows = append(rows, row)
	}
	httpx.JSON(w, http.StatusOK, rows)
}

type projectProgressRow struct {
	Project projectRefLite `json:"project"`
	Total   int            `json:"total"`
	Done    int            `json:"done"`
	Percent int            `json:"percent"`
}

// GET /api/dashboard/project-progress
func (a *API) ProjectProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cursor, err := a.Store.Projects().Find(ctx, bson.M{})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	type projLite struct {
		ID    primitive.ObjectID `bson:"_id"`
		Name  string             `bson:"name"`
		Key   string             `bson:"key"`
		Color string             `bson:"color"`
	}
	projects := []projLite{}
	for cursor.Next(ctx) {
		var p projLite
		if err := cursor.Decode(&p); err != nil {
			continue
		}
		projects = append(projects, p)
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"parentTask": nil}}},
		bson.D{{Key: "$group", Value: bson.M{
			"_id":   "$project",
			"total": bson.M{"$sum": 1},
			"done":  bson.M{"$sum": bson.M{"$cond": []interface{}{bson.M{"$eq": []interface{}{"$status", "done"}}, 1, 0}}},
		}}},
	}
	agg, err := a.Store.Tasks().Aggregate(ctx, pipeline)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer agg.Close(ctx)

	type aggRow struct {
		ID    primitive.ObjectID `bson:"_id"`
		Total int                `bson:"total"`
		Done  int                `bson:"done"`
	}
	byID := map[primitive.ObjectID]aggRow{}
	for agg.Next(ctx) {
		var ar aggRow
		if err := agg.Decode(&ar); err != nil {
			continue
		}
		byID[ar.ID] = ar
	}

	result := make([]projectProgressRow, 0, len(projects))
	for _, p := range projects {
		ar := byID[p.ID]
		percent := 0
		if ar.Total > 0 {
			percent = int(float64(ar.Done) / float64(ar.Total) * 100)
		}
		result = append(result, projectProgressRow{
			Project: projectRefLite{ID: p.ID, Name: p.Name, Key: p.Key, Color: p.Color},
			Total:   ar.Total, Done: ar.Done, Percent: percent,
		})
	}
	httpx.JSON(w, http.StatusOK, result)
}

type completionTrendRow struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// GET /api/dashboard/completion-trend - tasks completed per day, last 30 days.
func (a *API) CompletionTrend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	since := time.Now().AddDate(0, 0, -30)

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"completedAt": bson.M{"$gte": since}}}},
		bson.D{{Key: "$group", Value: bson.M{
			"_id":   bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$completedAt"}},
			"count": bson.M{"$sum": 1},
		}}},
		bson.D{{Key: "$sort", Value: bson.M{"_id": 1}}},
	}
	cursor, err := a.Store.Tasks().Aggregate(ctx, pipeline)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cursor.Close(ctx)

	rows := []completionTrendRow{}
	for cursor.Next(ctx) {
		var row struct {
			ID    string `bson:"_id"`
			Count int    `bson:"count"`
		}
		if err := cursor.Decode(&row); err != nil {
			continue
		}
		rows = append(rows, completionTrendRow{Date: row.ID, Count: row.Count})
	}
	httpx.JSON(w, http.StatusOK, rows)
}
