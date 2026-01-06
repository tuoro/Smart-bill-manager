package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"smart-bill-manager/internal/middleware"
	"smart-bill-manager/internal/services"
	"smart-bill-manager/internal/utils"
)

type TaskHandler struct {
	taskService *services.TaskService
}

func NewTaskHandler(taskService *services.TaskService) *TaskHandler {
	return &TaskHandler{taskService: taskService}
}

func (h *TaskHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/:id", h.Get)
	r.POST("/:id/cancel", h.Cancel)
}

func (h *TaskHandler) Get(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		utils.Error(c, http.StatusBadRequest, "缺少任务 id", nil)
		return
	}

	ownerUserID := middleware.GetEffectiveUserID(c)
	ctx, cancel := withReadTimeout(c)
	defer cancel()

	t, err := h.taskService.GetTaskForOwnerCtx(ctx, ownerUserID, id)
	if err != nil {
		if handleReadTimeoutError(c, err) {
			return
		}
		utils.Error(c, http.StatusNotFound, "任务不存在", err)
		return
	}

	var result any
	if t.ResultJSON != nil && strings.TrimSpace(*t.ResultJSON) != "" {
		_ = json.Unmarshal([]byte(*t.ResultJSON), &result)
	}

	utils.SuccessData(c, gin.H{
		"id":         t.ID,
		"type":       t.Type,
		"status":     t.Status,
		"created_by": t.CreatedBy,
		"target_id":  t.TargetID,
		"error":      t.Error,
		"result":     result,
		"created_at": t.CreatedAt,
		"updated_at": t.UpdatedAt,
	})
}

func (h *TaskHandler) Cancel(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		utils.Error(c, http.StatusBadRequest, "缺少任务 id", nil)
		return
	}
	ownerUserID := middleware.GetEffectiveUserID(c)
	if ownerUserID == "" {
		utils.Error(c, http.StatusUnauthorized, "未登录", nil)
		return
	}

	if err := h.taskService.CancelTask(id, ownerUserID); err != nil {
		utils.Error(c, http.StatusBadRequest, "取消任务失败", err)
		return
	}
	utils.SuccessData(c, gin.H{"canceled": true})
}
