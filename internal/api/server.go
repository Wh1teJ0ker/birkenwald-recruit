package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/platform"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	DB     *pgxpool.Pool
	Config platform.Config
}

type User struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	CSRF  string `json:"csrf_token"`
}

func (s *Server) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.SetTrustedProxies(nil)
	r.GET("/healthz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := s.DB.Ping(ctx); err != nil {
			fail(c, 503, "database unavailable")
			return
		}
		c.JSON(200, gin.H{"ok": true, "version": platform.Version})
	})
	r.GET("/api/v1/version", func(c *gin.Context) { c.JSON(200, gin.H{"version": platform.Version}) })
	if _, err := os.Stat(filepath.Join(s.Config.WebDist, "index.html")); err == nil {
		r.Static("/assets", filepath.Join(s.Config.WebDist, "assets"))
		r.GET("/", func(c *gin.Context) { c.File(filepath.Join(s.Config.WebDist, "index.html")) })
	} else {
		r.GET("/", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "React frontend not built; run npm run dev in frontend/", "version": platform.Version})
		})
	}

	api := r.Group("/api/v1")
	api.POST("/auth/register", s.register)
	api.POST("/auth/login", s.login)
	private := api.Group("")
	private.Use(s.authenticate)
	private.GET("/me", func(c *gin.Context) { c.JSON(200, currentUser(c)) })
	private.POST("/auth/logout", s.logout)
	private.GET("/campaigns", s.listCampaigns)
	private.GET("/campaigns/:id/tasks", s.listTasks)
	private.POST("/campaigns/:id/selections", s.selectTask)
	private.GET("/me/selections", s.mySelections)
	private.POST("/files", s.uploadFile)
	private.GET("/files/:id/download", s.downloadFile)
	private.POST("/selections/:id/submissions", s.submit)
	admin := private.Group("/admin")
	admin.Use(requireAdmin)
	admin.GET("/plugins", s.listPlugins)
	admin.POST("/campaigns", s.createCampaign)
	admin.POST("/tasks", s.createTask)
	admin.GET("/submissions", s.adminSubmissions)
	admin.POST("/evaluations/:id/retry", s.retryEvaluation)
	return r
}

func fail(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}

func parseID(c *gin.Context, param string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || id <= 0 {
		fail(c, 400, "invalid id")
		return 0, false
	}
	return id, true
}

func sameOrigin(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, c.Request.Host)
}

func (s *Server) setSession(c *gin.Context, id int64) error {
	token, err := platform.RandomToken()
	if err != nil {
		return err
	}
	csrf, err := platform.RandomToken()
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(c.Request.Context(), "INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,now()+interval '7 days')", platform.TokenHash(token), id, csrf)
	if err != nil {
		return err
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("bw_session", token, 7*24*3600, "/", "", s.Config.CookieSecure, true)
	return nil
}

func (s *Server) authenticate(c *gin.Context) {
	token, err := c.Cookie("bw_session")
	if err != nil || token == "" {
		fail(c, 401, "login required")
		return
	}
	var u User
	err = s.DB.QueryRow(c.Request.Context(), `SELECT u.id,u.email,u.display_name,u.role,s.csrf_token
        FROM sessions s JOIN users u ON u.id=s.user_id
        WHERE s.token_hash=$1 AND s.expires_at>now()`, platform.TokenHash(token)).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CSRF)
	if err != nil {
		fail(c, 401, "session expired")
		return
	}
	if c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.Request.Method != "OPTIONS" {
		if !sameOrigin(c) || c.GetHeader("X-CSRF-Token") != u.CSRF {
			fail(c, 403, "invalid CSRF token")
			return
		}
	}
	c.Set("user", u)
	c.Next()
}

func currentUser(c *gin.Context) User { return c.MustGet("user").(User) }

func requireAdmin(c *gin.Context) {
	if currentUser(c).Role != "admin" {
		fail(c, 403, "admin required")
		return
	}
	c.Next()
}

type authInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func (s *Server) register(c *gin.Context) {
	if !sameOrigin(c) {
		fail(c, 403, "invalid origin")
		return
	}
	var in authInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	addr, err := mail.ParseAddress(in.Email)
	if err != nil || addr.Address != in.Email || len(in.Email) > 254 || len(in.Name) < 1 || len(in.Name) > 100 || len(in.Password) < 12 {
		fail(c, 422, "valid email, name and password of at least 12 characters required")
		return
	}
	hash, err := platform.HashPassword(in.Password)
	if err != nil {
		fail(c, 500, "registration failed")
		return
	}
	var id int64
	err = s.DB.QueryRow(c.Request.Context(), `INSERT INTO users(email,display_name,password_hash,role)
        VALUES($1,$2,$3,'student') ON CONFLICT(email) DO NOTHING RETURNING id`, in.Email, in.Name, hash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 409, "email already registered")
		return
	}
	if err != nil {
		fail(c, 500, "registration failed")
		return
	}
	if err = s.setSession(c, id); err != nil {
		fail(c, 500, "session failed")
		return
	}
	c.JSON(201, gin.H{"id": id, "role": "student"})
}

func (s *Server) login(c *gin.Context) {
	if !sameOrigin(c) {
		fail(c, 403, "invalid origin")
		return
	}
	var in authInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	var id int64
	var hash, role string
	err := s.DB.QueryRow(c.Request.Context(), "SELECT id,password_hash,role FROM users WHERE email=$1", strings.ToLower(strings.TrimSpace(in.Email))).Scan(&id, &hash, &role)
	if err != nil || !platform.CheckPassword(hash, in.Password) {
		fail(c, 401, "invalid credentials")
		return
	}
	if err = s.setSession(c, id); err != nil {
		fail(c, 500, "session failed")
		return
	}
	c.JSON(200, gin.H{"id": id, "role": role})
}

func (s *Server) logout(c *gin.Context) {
	token, _ := c.Cookie("bw_session")
	_, _ = s.DB.Exec(c.Request.Context(), "DELETE FROM sessions WHERE token_hash=$1", platform.TokenHash(token))
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("bw_session", "", -1, "/", "", s.Config.CookieSecure, true)
	c.JSON(200, gin.H{"ok": true})
}

type campaignInput struct {
	Name     string    `json:"name"`
	OpensAt  time.Time `json:"opens_at"`
	ClosesAt time.Time `json:"closes_at"`
	Open     bool      `json:"open"`
}

func (s *Server) createCampaign(c *gin.Context) {
	var in campaignInput
	if err := c.ShouldBindJSON(&in); err != nil || strings.TrimSpace(in.Name) == "" || !in.OpensAt.Before(in.ClosesAt) {
		fail(c, 422, "invalid campaign")
		return
	}
	status := "draft"
	if in.Open {
		status = "open"
	}
	var id int64
	err := s.DB.QueryRow(c.Request.Context(), "INSERT INTO campaigns(name,status,opens_at,closes_at) VALUES($1,$2,$3,$4) RETURNING id", strings.TrimSpace(in.Name), status, in.OpensAt, in.ClosesAt).Scan(&id)
	if err != nil {
		fail(c, 500, "create campaign failed")
		return
	}
	s.audit(c, "campaign.create", "campaign", id)
	c.JSON(201, gin.H{"id": id, "status": status})
}

func (s *Server) listCampaigns(c *gin.Context) {
	admin := currentUser(c).Role == "admin"
	rows, err := s.DB.Query(c.Request.Context(), `SELECT id,name,status,opens_at,closes_at FROM campaigns
        WHERE status='open' OR $1 ORDER BY id DESC`, admin)
	if err != nil {
		fail(c, 500, "list campaigns failed")
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id int64
		var name, status string
		var opens, closes time.Time
		if err := rows.Scan(&id, &name, &status, &opens, &closes); err != nil {
			fail(c, 500, "list campaigns failed")
			return
		}
		items = append(items, gin.H{"id": id, "name": name, "status": status, "opens_at": opens, "closes_at": closes})
	}
	c.JSON(200, items)
}

type taskInput struct {
	CampaignID      int64           `json:"campaign_id"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	DeadlineAt      time.Time       `json:"deadline_at"`
	Capacity        *int32          `json:"capacity"`
	PluginVersionID int64           `json:"plugin_version_id"`
	PluginConfig    json.RawMessage `json:"plugin_config"`
	Publish         bool            `json:"publish"`
}

func (s *Server) createTask(c *gin.Context) {
	var in taskInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	if in.CampaignID <= 0 || in.PluginVersionID <= 0 || strings.TrimSpace(in.Title) == "" || len(in.Title) > 200 || (in.Capacity != nil && *in.Capacity <= 0) {
		fail(c, 422, "invalid task")
		return
	}
	if len(in.PluginConfig) == 0 {
		in.PluginConfig = []byte("{}")
	}
	if !json.Valid(in.PluginConfig) || in.PluginConfig[0] != '{' {
		fail(c, 422, "plugin_config must be an object")
		return
	}
	var opens, closes time.Time
	err := s.DB.QueryRow(c.Request.Context(), "SELECT opens_at,closes_at FROM campaigns WHERE id=$1", in.CampaignID).Scan(&opens, &closes)
	if err != nil || in.DeadlineAt.Before(opens) || in.DeadlineAt.After(closes) {
		fail(c, 422, "deadline must be within campaign dates")
		return
	}
	var enabled bool
	err = s.DB.QueryRow(c.Request.Context(), "SELECT enabled FROM plugin_versions WHERE id=$1", in.PluginVersionID).Scan(&enabled)
	if err != nil || !enabled {
		fail(c, 422, "plugin not available")
		return
	}
	status := "draft"
	if in.Publish {
		status = "published"
	}
	var id int64
	err = s.DB.QueryRow(c.Request.Context(), `INSERT INTO tasks(campaign_id,title,description,status,capacity,deadline_at,plugin_version_id,plugin_config)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		in.CampaignID, strings.TrimSpace(in.Title), in.Description, status, in.Capacity, in.DeadlineAt, in.PluginVersionID, in.PluginConfig).Scan(&id)
	if err != nil {
		fail(c, 500, "create task failed")
		return
	}
	s.audit(c, "task.create", "task", id)
	c.JSON(201, gin.H{"id": id, "status": status})
}

func (s *Server) listTasks(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	admin := currentUser(c).Role == "admin"
	rows, err := s.DB.Query(c.Request.Context(), `SELECT t.id,t.title,t.description,t.status,t.capacity,t.selected_count,t.deadline_at,t.plugin_version_id
        FROM tasks t WHERE t.campaign_id=$1 AND (t.status='published' OR $2) ORDER BY t.id`, id, admin)
	if err != nil {
		fail(c, 500, "list tasks failed")
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var taskID, pluginID int64
		var title, description, status string
		var capacity *int32
		var selected int32
		var deadline time.Time
		if err := rows.Scan(&taskID, &title, &description, &status, &capacity, &selected, &deadline, &pluginID); err != nil {
			fail(c, 500, "list tasks failed")
			return
		}
		items = append(items, gin.H{"id": taskID, "title": title, "description": description, "status": status, "capacity": capacity, "selected_count": selected, "deadline_at": deadline, "plugin_version_id": pluginID})
	}
	c.JSON(200, items)
}

func (s *Server) listPlugins(c *gin.Context) {
	rows, err := s.DB.Query(c.Request.Context(), "SELECT id,name,version,image_ref,config_schema FROM plugin_versions WHERE enabled ORDER BY name,version")
	if err != nil {
		fail(c, 500, "list plugins failed")
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id int64
		var name, version, image string
		var schema json.RawMessage
		if err := rows.Scan(&id, &name, &version, &image, &schema); err != nil {
			fail(c, 500, "list plugins failed")
			return
		}
		items = append(items, gin.H{"id": id, "name": name, "version": version, "image_ref": image, "config_schema": schema})
	}
	c.JSON(200, items)
}

func (s *Server) audit(c *gin.Context, action, target string, id int64) {
	user := currentUser(c)
	_, _ = s.DB.Exec(c.Request.Context(), "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,$2,$3,$4)", user.ID, action, target, id)
}
