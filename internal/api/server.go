package api

import (
	"net/http"
	"strings"

	"github.com/automatizia/autowab/internal/whatsapp"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type Server struct {
	wac   *whatsapp.Client
	token string
	log   *zap.Logger
	r     *gin.Engine
}

func New(wac *whatsapp.Client, token string, log *zap.Logger) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	s := &Server{wac: wac, token: token, log: log, r: r}
	s.routes()
	return s
}

func (s *Server) Run(addr string) error {
	return s.r.Run(addr)
}

func (s *Server) auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		hdr := c.GetHeader("Authorization")
		if !strings.HasPrefix(hdr, "Bearer ") || strings.TrimPrefix(hdr, "Bearer ") != s.token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func (s *Server) routes() {
	s.r.GET("/health", s.health)

	api := s.r.Group("/api/v1", s.auth())
	api.GET("/status", s.status)
	api.GET("/qr", s.qr)
	api.POST("/send", s.send)
	api.GET("/messages", s.messages)
}

// GET /health — no auth
func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"service":   "autowab",
		"connected": s.wac.IsConnected(),
		"loggedIn":  s.wac.IsLoggedIn(),
	})
}

// GET /api/v1/status
func (s *Server) status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"connected": s.wac.IsConnected(),
		"loggedIn":  s.wac.IsLoggedIn(),
	})
}

// GET /api/v1/qr — streams the latest QR code (for first-time auth)
func (s *Server) qr(c *gin.Context) {
	if s.wac.IsLoggedIn() {
		c.JSON(http.StatusOK, gin.H{"loggedIn": true, "message": "Already authenticated"})
		return
	}

	select {
	case code := <-s.wac.QRChan:
		c.JSON(http.StatusOK, gin.H{"qr": code})
	default:
		c.JSON(http.StatusAccepted, gin.H{"message": "No QR ready yet, try again in a moment"})
	}
}

// POST /api/v1/send
// Body: { "to": "521XXXXXXXXXX", "text": "Hello!" }
func (s *Server) send(c *gin.Context) {
	var body struct {
		To   string `json:"to"   binding:"required"`
		Text string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	msgID, err := s.wac.SendText(body.To, body.Text)
	if err != nil {
		s.log.Error("send failed", zap.String("to", body.To), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "messageId": msgID})
}

// GET /api/v1/messages?limit=20
func (s *Server) messages(c *gin.Context) {
	limit := 20
	msgs, err := s.wac.GetMessages(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": msgs})
}
