//go:build unit

package admin

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/repository"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

func configureBulkLiveListBridge(router *gin.Engine, controls *gin.RouterGroup, fixture *bulkLiveFixture) {
	userRepository := repository.NewUserRepository(fixture.client, fixture.db)
	groupRepository := repository.NewGroupRepository(fixture.client, fixture.db)
	adminService := service.NewAdminService(
		&config.Config{}, userRepository, nil, groupRepository.(service.AdminGroupRepository),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, fixture.client,
		nil, nil, fixture.repo, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	router.GET("/api/v1/admin/groups/all", fixture.auth, NewGroupHandler(adminService, nil, nil).GetAll)
	router.GET("/api/v1/admin/users", fixture.auth, NewUserHandler(adminService, nil, nil, nil, nil, nil, nil).List)
	router.GET("/api/v1/admin/usage/search-users", fixture.auth, NewUsageHandler(nil, nil, adminService, nil).SearchUsers)
	router.GET("/api/v1/admin/subscriptions", fixture.auth, func(ctx *gin.Context) {
		query := ctx.Request.URL.Query()
		if supplied := query.Get("group_id"); supplied != "" && supplied != strconv.FormatInt(fixture.groupID, 10) {
			ctx.AbortWithStatus(403)
			return
		}
		query.Set("group_id", strconv.FormatInt(fixture.groupID, 10))
		ctx.Request.URL.RawQuery = query.Encode()
		fixture.handler.List(ctx)
	})
	router.POST("/api/v1/admin/subscriptions/bulk-assign", fixture.auth, func(ctx *gin.Context) {
		var input BulkAssignSubscriptionRequest
		if err := ctx.ShouldBindJSON(&input); err != nil {
			ctx.AbortWithStatus(400)
			return
		}
		if input.GroupID != fixture.groupID {
			ctx.AbortWithStatus(403)
			return
		}
		for _, userID := range input.UserIDs {
			allowed := false
			for _, ownID := range fixture.users {
				allowed = allowed || ownID == userID
			}
			if !allowed {
				ctx.AbortWithStatus(403)
				return
			}
		}
		body, _ := json.Marshal(input)
		ctx.Request.Body = ioNopCloser{bytes.NewReader(body)}
		fixture.handler.BulkAssign(ctx)
	})
	controls.POST("/seed-list", func(ctx *gin.Context) {
		if len(fixture.ids) > 3 {
			ctx.Status(200)
			return
		}
		for index := 0; index < 12; index++ {
			user, err := fixture.client.User.Create().SetEmail(uuid.NewString() + "@bulk-live.invalid").SetPasswordHash("synthetic-fixture-no-login").SetRole("user").Save(ctx.Request.Context())
			if err != nil {
				ctx.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
				return
			}
			fixture.users = append(fixture.users, user.ID)
			subscription, err := fixture.client.UserSubscription.Create().SetUserID(user.ID).SetGroupID(fixture.groupID).SetStartsAt(time.Now()).SetExpiresAt(fixture.initial).Save(ctx.Request.Context())
			if err != nil {
				ctx.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
				return
			}
			fixture.ids = append(fixture.ids, subscription.ID)
		}
		ctx.Status(200)
	})
	controls.GET("/list-state", func(ctx *gin.Context) {
		users := []gin.H{}
		for _, userID := range fixture.users {
			user, err := userRepository.GetByID(ctx.Request.Context(), userID)
			if err != nil {
				ctx.AbortWithStatus(500)
				return
			}
			users = append(users, gin.H{"id": user.ID, "email": user.Email, "role": user.Role})
		}
		group, err := groupRepository.GetByID(ctx.Request.Context(), fixture.groupID)
		if err != nil {
			ctx.AbortWithStatus(500)
			return
		}
		rows, err := fixture.db.QueryContext(ctx.Request.Context(), `SELECT id,user_id,expires_at,deleted_at FROM user_subscriptions WHERE group_id=$1 ORDER BY id`, fixture.groupID)
		if err != nil {
			ctx.AbortWithStatus(500)
			return
		}
		defer rows.Close()
		subscriptions := []gin.H{}
		for rows.Next() {
			var subscriptionID, userID int64
			var expires time.Time
			var deleted *time.Time
			if err := rows.Scan(&subscriptionID, &userID, &expires, &deleted); err != nil {
				ctx.AbortWithStatus(500)
				return
			}
			subscriptions = append(subscriptions, gin.H{"id": subscriptionID, "user_id": userID, "expires_at": expires, "deleted_at": deleted})
		}
		if rows.Err() != nil {
			ctx.AbortWithStatus(500)
			return
		}
		ctx.JSON(200, gin.H{"users": users, "subscriptions": subscriptions, "group_id": fixture.groupID, "group_name": group.Name})
	})
}
