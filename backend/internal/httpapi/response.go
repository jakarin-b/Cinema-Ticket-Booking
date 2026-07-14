package httpapi

import (
	"net/http"

	"github.com/cinema-ticket-booking/backend/internal/service"
	"github.com/gin-gonic/gin"
)

func success(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"data": data, "meta": gin.H{"request_id": requestID(c)}})
}
func list(c *gin.Context, data any, page, limit, total int64) {
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": gin.H{"page": page, "limit": limit, "total": total, "request_id": requestID(c)}})
}
func fail(c *gin.Context, err error) {
	if problem, ok := err.(*service.Error); ok {
		c.JSON(problem.Status, gin.H{"error": gin.H{"code": problem.Code, "message": problem.Message, "details": safeDetails(problem.Details)}, "meta": gin.H{"request_id": requestID(c)}})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "INTERNAL_ERROR", "message": "An unexpected error occurred.", "details": gin.H{}}, "meta": gin.H{"request_id": requestID(c)}})
}
func safeDetails(details map[string]any) map[string]any {
	if details == nil {
		return map[string]any{}
	}
	return details
}
func requestID(c *gin.Context) string {
	value, _ := c.Get("request_id")
	id, _ := value.(string)
	return id
}
