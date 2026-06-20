package media

import (
	"github.com/gin-gonic/gin"

	"fyms/internal/appstate"
)

type AppState = appstate.AppState

func GetState(c *gin.Context) *AppState {
	return appstate.GetState(c)
}
