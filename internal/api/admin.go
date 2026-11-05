package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/platform"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type siteConfig struct {
	SiteName            string `json:"site_name"`
	SeasonYear          string `json:"season_year"`
	RegistrationEnabled bool   `json:"registration_enabled"`
}

func loadSiteConfig(ctx context.Context, pool *pgxpool.Pool) (siteConfig, error) {
	var cfg siteConfig
	var registration string
	err := pool.QueryRow(ctx, `SELECT
        COALESCE((SELECT value FROM app_settings WHERE key='site_name'), 'Birkenwald'),
        COALESCE((SELECT value FROM app_settings WHERE key='season_year'), $1),
        COALESCE((SELECT value FROM app_settings WHERE key='registration_enabled'), 'true')`,
		strconv.Itoa(time.Now().Year())).Scan(&cfg.SiteName, &cfg.SeasonYear, &registration)
	if err != nil {
		return cfg, err
	}
	cfg.RegistrationEnabled = strings.EqualFold(registration, "true")
	return cfg, nil
}

func registrationEnabled(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var value string
	err := pool.QueryRow(ctx, "SELECT value FROM app_settings WHERE key='registration_enabled'").Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return strings.EqualFold(value, "true"), nil
}

func (s *Server) publicConfig(c *gin.Context) {
	cfg, err := loadSiteConfig(c.Request.Context(), s.DB)
	if err != nil {
		fail(c, 500, "load site config failed")
		return
	}
	c.JSON(200, cfg)
}

func (s *Server) adminUsers(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	role := strings.TrimSpace(c.Query("role"))
	status := strings.TrimSpace(c.Query("status"))
	if role != "" && role != "student" && role != "admin" {
		fail(c, 422, "invalid role")
		return
	}
	if status != "" && status != "active" && status != "disabled" {
		fail(c, 422, "invalid status")
		return
	}
	rows, err := s.DB.Query(c.Request.Context(), `SELECT u.id,u.email,u.display_name,u.role,u.disabled,u.created_at,
        (SELECT count(*) FROM selections s WHERE s.student_id=u.id),
        (SELECT count(*) FROM submissions sub JOIN selections s ON s.id=sub.selection_id WHERE s.student_id=u.id)
        FROM users u
        WHERE ($1='' OR u.email ILIKE '%'||$1||'%' OR u.display_name ILIKE '%'||$1||'%')
          AND ($2='' OR u.role=$2)
          AND ($3='' OR ($3='active' AND NOT u.disabled) OR ($3='disabled' AND u.disabled))
        ORDER BY u.disabled, u.id DESC LIMIT 500`, query, role, status)
	if err != nil {
		fail(c, 500, "list users failed")
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id, selections, submissions int64
		var email, name, userRole string
		var disabled bool
		var createdAt time.Time
		if err := rows.Scan(&id, &email, &name, &userRole, &disabled, &createdAt, &selections, &submissions); err != nil {
			fail(c, 500, "list users failed")
			return
		}
		items = append(items, gin.H{
			"id": id, "email": email, "name": name, "role": userRole, "disabled": disabled,
			"created_at": createdAt, "selection_count": selections, "submission_count": submissions,
		})
	}
	if err := rows.Err(); err != nil {
		fail(c, 500, "list users failed")
		return
	}
	c.JSON(200, items)
}

type userUpdateInput struct {
	Role     *string `json:"role"`
	Disabled *bool   `json:"disabled"`
}

func (s *Server) updateUser(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in userUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	if in.Role == nil && in.Disabled == nil {
		fail(c, 422, "no user changes provided")
		return
	}
	if in.Role != nil && *in.Role != "student" && *in.Role != "admin" {
		fail(c, 422, "invalid role")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		fail(c, 500, "update user failed")
		return
	}
	defer tx.Rollback(ctx)
	var role string
	var disabled bool
	err = tx.QueryRow(ctx, "SELECT role,disabled FROM users WHERE id=$1 FOR UPDATE", id).Scan(&role, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "user not found")
		return
	}
	if err != nil {
		fail(c, 500, "update user failed")
		return
	}
	newRole, newDisabled := role, disabled
	if in.Role != nil {
		newRole = *in.Role
	}
	if in.Disabled != nil {
		newDisabled = *in.Disabled
	}
	actor := currentUser(c)
	if id == actor.ID && (newRole != "admin" || newDisabled) {
		fail(c, 409, "cannot lock your own admin account")
		return
	}
	if role == "admin" && !disabled && (newRole != "admin" || newDisabled) {
		var others int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM users WHERE role='admin' AND NOT disabled AND id<>$1", id).Scan(&others); err != nil {
			fail(c, 500, "update user failed")
			return
		}
		if others == 0 {
			fail(c, 409, "at least one active admin required")
			return
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE users SET role=$2,disabled=$3 WHERE id=$1", id, newRole, newDisabled); err != nil {
		fail(c, 500, "update user failed")
		return
	}
	if newDisabled || newRole != role {
		if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1", id); err != nil {
			fail(c, 500, "update user failed")
			return
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'user.update','user',$2)", actor.ID, id); err != nil {
		fail(c, 500, "update user failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		fail(c, 500, "update user failed")
		return
	}
	c.JSON(200, gin.H{"id": id, "role": newRole, "disabled": newDisabled})
}

type campaignUpdateInput struct {
	Status string `json:"status"`
}

func (s *Server) updateCampaign(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in campaignUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil || (in.Status != "draft" && in.Status != "open" && in.Status != "closed") {
		fail(c, 422, "invalid campaign status")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		fail(c, 500, "update campaign failed")
		return
	}
	defer tx.Rollback(ctx)
	var updated int64
	err = tx.QueryRow(ctx, "UPDATE campaigns SET status=$2 WHERE id=$1 RETURNING id", id, in.Status).Scan(&updated)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "campaign not found")
		return
	}
	if err != nil {
		fail(c, 500, "update campaign failed")
		return
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'campaign.update','campaign',$2)", currentUser(c).ID, id); err != nil {
		fail(c, 500, "update campaign failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		fail(c, 500, "update campaign failed")
		return
	}
	c.JSON(200, gin.H{"id": id, "status": in.Status})
}

type taskUpdateInput struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	DeadlineAt  time.Time `json:"deadline_at"`
	Capacity    *int32    `json:"capacity"`
	Status      string    `json:"status"`
}

func (s *Server) updateTask(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in taskUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || len(title) > 200 || (in.Capacity != nil && *in.Capacity <= 0) ||
		(in.Status != "draft" && in.Status != "published" && in.Status != "closed") || in.DeadlineAt.IsZero() {
		fail(c, 422, "invalid task")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		fail(c, 500, "update task failed")
		return
	}
	defer tx.Rollback(ctx)
	var selected int32
	var opens, closes time.Time
	err = tx.QueryRow(ctx, `SELECT t.selected_count,c.opens_at,c.closes_at
        FROM tasks t JOIN campaigns c ON c.id=t.campaign_id
        WHERE t.id=$1 FOR UPDATE OF t`, id).Scan(&selected, &opens, &closes)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "task not found")
		return
	}
	if err != nil {
		fail(c, 500, "update task failed")
		return
	}
	if in.DeadlineAt.Before(opens) || in.DeadlineAt.After(closes) {
		fail(c, 422, "deadline must be within campaign dates")
		return
	}
	if in.Capacity != nil && *in.Capacity < selected {
		fail(c, 409, "capacity below selected count")
		return
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET title=$2,description=$3,deadline_at=$4,capacity=$5,status=$6 WHERE id=$1`,
		id, title, in.Description, in.DeadlineAt, in.Capacity, in.Status); err != nil {
		fail(c, 500, "update task failed")
		return
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'task.update','task',$2)", currentUser(c).ID, id); err != nil {
		fail(c, 500, "update task failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		fail(c, 500, "update task failed")
		return
	}
	c.JSON(200, gin.H{"id": id, "status": in.Status})
}

func (s *Server) mailConfig(ctx context.Context) (platform.Config, error) {
	cfg := s.Config
	rows, err := s.DB.Query(ctx, `SELECT key,value FROM app_settings
        WHERE key IN ('smtp_host','smtp_port','smtp_username','smtp_password','smtp_from','email_dev_mode')`)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return cfg, err
		}
		switch key {
		case "smtp_host":
			cfg.SMTPHost = value
		case "smtp_port":
			if value == "" {
				continue
			}
			port, err := strconv.Atoi(value)
			if err != nil || port < 1 || port > 65535 {
				return cfg, errors.New("invalid stored smtp port")
			}
			cfg.SMTPPort = port
		case "smtp_username":
			cfg.SMTPUsername = value
		case "smtp_password":
			cfg.SMTPPassword = value
		case "smtp_from":
			cfg.SMTPFrom = value
		case "email_dev_mode":
			cfg.EmailDevMode = strings.EqualFold(value, "true")
		}
	}
	return cfg, rows.Err()
}

func (s *Server) settingsPayload(ctx context.Context) (gin.H, error) {
	cfg, err := loadSiteConfig(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	mailCfg, err := s.mailConfig(ctx)
	if err != nil {
		return nil, err
	}
	var passwordSaved bool
	if err := s.DB.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM app_settings WHERE key='smtp_password' AND value<>'')").Scan(&passwordSaved); err != nil {
		return nil, err
	}
	var dbVersion string
	var dbTime time.Time
	var students, admins, disabledUsers, openCampaigns, publishedTasks, submissions, failedJobs int64
	err = s.DB.QueryRow(ctx, `SELECT current_setting('server_version'),now(),
        (SELECT count(*) FROM users WHERE role='student'),
        (SELECT count(*) FROM users WHERE role='admin'),
        (SELECT count(*) FROM users WHERE disabled),
        (SELECT count(*) FROM campaigns WHERE status='open'),
        (SELECT count(*) FROM tasks WHERE status='published'),
        (SELECT count(*) FROM submissions),
        (SELECT count(*) FROM evaluation_jobs WHERE status='failed')`).
		Scan(&dbVersion, &dbTime, &students, &admins, &disabledUsers, &openCampaigns, &publishedTasks, &submissions, &failedJobs)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"site_name":            cfg.SiteName,
		"season_year":          cfg.SeasonYear,
		"registration_enabled": cfg.RegistrationEnabled,
		"version":              platform.Version,
		"go_version":           runtime.Version(),
		"database_version":     dbVersion,
		"database_time":        dbTime,
		"email_dev_mode":       mailCfg.EmailDevMode,
		"smtp_configured":      mailCfg.SMTPHost != "",
		"smtp_host":            mailCfg.SMTPHost,
		"smtp_port":            mailCfg.SMTPPort,
		"smtp_username":        mailCfg.SMTPUsername,
		"smtp_from":            mailCfg.SMTPFrom,
		"smtp_password_set":    mailCfg.SMTPPassword != "",
		"smtp_password_saved":  passwordSaved,
		"cookie_secure":        s.Config.CookieSecure,
		"plugin_timeout":       s.Config.PluginTimeout.String(),
		"counts": gin.H{
			"students": students, "admins": admins, "disabled_users": disabledUsers,
			"open_campaigns": openCampaigns, "published_tasks": publishedTasks,
			"submissions": submissions, "failed_evaluations": failedJobs,
		},
	}, nil
}

func (s *Server) adminSettings(c *gin.Context) {
	payload, err := s.settingsPayload(c.Request.Context())
	if err != nil {
		fail(c, 500, "load settings failed")
		return
	}
	c.JSON(200, payload)
}

type settingsInput struct {
	SiteName            *string `json:"site_name"`
	SeasonYear          *string `json:"season_year"`
	RegistrationEnabled *bool   `json:"registration_enabled"`
	SMTPHost            *string `json:"smtp_host"`
	SMTPPort            any     `json:"smtp_port"`
	SMTPUsername        *string `json:"smtp_username"`
	SMTPPassword        *string `json:"smtp_password"`
	SMTPFrom            *string `json:"smtp_from"`
	EmailDevMode        *bool   `json:"email_dev_mode"`
	ClearSMTPPassword   *bool   `json:"clear_smtp_password"`
}

func (s *Server) updateSettings(c *gin.Context) {
	var in settingsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	if in.SiteName == nil && in.SeasonYear == nil && in.RegistrationEnabled == nil &&
		in.SMTPHost == nil && in.SMTPPort == nil && in.SMTPUsername == nil && in.SMTPPassword == nil &&
		in.SMTPFrom == nil && in.EmailDevMode == nil && in.ClearSMTPPassword == nil {
		fail(c, 422, "no settings provided")
		return
	}
	changes := make(map[string]string)
	deletes := make(map[string]bool)
	if in.SiteName != nil {
		name := strings.TrimSpace(*in.SiteName)
		if utf8.RuneCountInString(name) == 0 || utf8.RuneCountInString(name) > 40 {
			fail(c, 422, "site_name must be 1-40 characters")
			return
		}
		changes["site_name"] = name
	}
	if in.SeasonYear != nil {
		year := strings.TrimSpace(*in.SeasonYear)
		value, err := strconv.Atoi(year)
		if err != nil || len(year) != 4 || value < 2000 || value > 2100 {
			fail(c, 422, "season_year must be between 2000 and 2100")
			return
		}
		changes["season_year"] = year
	}
	if in.RegistrationEnabled != nil {
		changes["registration_enabled"] = strconv.FormatBool(*in.RegistrationEnabled)
	}
	if in.SMTPHost != nil {
		host := strings.TrimSpace(*in.SMTPHost)
		if utf8.RuneCountInString(host) > 255 {
			fail(c, 422, "smtp_host is too long")
			return
		}
		if host == "" {
			deletes["smtp_host"] = true
		} else {
			changes["smtp_host"] = host
		}
	}
	if in.SMTPPort != nil {
		port := ""
		switch value := in.SMTPPort.(type) {
		case string:
			port = strings.TrimSpace(value)
		case float64:
			port = strconv.FormatFloat(value, 'f', -1, 64)
		default:
			fail(c, 422, "invalid smtp_port")
			return
		}
		if port == "" {
			deletes["smtp_port"] = true
		} else {
			number, err := strconv.Atoi(port)
			if err != nil || number < 1 || number > 65535 {
				fail(c, 422, "smtp_port must be between 1 and 65535")
				return
			}
			changes["smtp_port"] = strconv.Itoa(number)
		}
	}
	if in.SMTPUsername != nil {
		username := strings.TrimSpace(*in.SMTPUsername)
		if utf8.RuneCountInString(username) > 255 {
			fail(c, 422, "smtp_username is too long")
			return
		}
		if username == "" {
			deletes["smtp_username"] = true
		} else {
			changes["smtp_username"] = username
		}
	}
	if in.SMTPFrom != nil {
		from := strings.TrimSpace(*in.SMTPFrom)
		if from == "" {
			deletes["smtp_from"] = true
		} else {
			address, err := mail.ParseAddress(from)
			if err != nil {
				fail(c, 422, "invalid smtp_from")
				return
			}
			changes["smtp_from"] = address.Address
		}
	}
	if in.ClearSMTPPassword != nil && *in.ClearSMTPPassword {
		deletes["smtp_password"] = true
	}
	if in.SMTPPassword != nil && *in.SMTPPassword != "" {
		if in.ClearSMTPPassword != nil && *in.ClearSMTPPassword {
			fail(c, 422, "cannot set and clear smtp_password together")
			return
		}
		if len(*in.SMTPPassword) > 512 {
			fail(c, 422, "smtp_password is too long")
			return
		}
		changes["smtp_password"] = *in.SMTPPassword
	}
	if in.EmailDevMode != nil {
		changes["email_dev_mode"] = strconv.FormatBool(*in.EmailDevMode)
	}
	if len(changes) == 0 && len(deletes) == 0 {
		fail(c, 422, "no settings provided")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		fail(c, 500, "update settings failed")
		return
	}
	defer tx.Rollback(ctx)
	for key := range deletes {
		if _, err = tx.Exec(ctx, "DELETE FROM app_settings WHERE key=$1", key); err != nil {
			fail(c, 500, "update settings failed")
			return
		}
	}
	for key, value := range changes {
		if _, err = tx.Exec(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES($1,$2,now())
            ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=now()`, key, value); err != nil {
			fail(c, 500, "update settings failed")
			return
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'settings.update','settings',0)", currentUser(c).ID); err != nil {
		fail(c, 500, "update settings failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		fail(c, 500, "update settings failed")
		return
	}
	payload, err := s.settingsPayload(ctx)
	if err != nil {
		fail(c, 500, "load settings failed")
		return
	}
	c.JSON(200, payload)
}

type pluginInput struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	LibraryPath  string          `json:"library_path"`
	ConfigSchema json.RawMessage `json:"config_schema"`
	Enabled      *bool           `json:"enabled"`
}

func validPluginSchema(schema json.RawMessage) bool {
	return len(schema) > 0 && len(schema) <= 16384 && json.Valid(schema) && schema[0] == '{'
}

func validPluginLibraryPath(libraryPath string) bool {
	return libraryPath != "" && len(libraryPath) <= 500 &&
		!strings.ContainsAny(libraryPath, " \t\r\n") &&
		strings.HasSuffix(strings.ToLower(libraryPath), ".so")
}

func (s *Server) createPlugin(c *gin.Context) {
	var in pluginInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	name := strings.TrimSpace(in.Name)
	version := strings.TrimSpace(in.Version)
	libraryPath := strings.TrimSpace(in.LibraryPath)
	if name == "" || version == "" ||
		utf8.RuneCountInString(name) > 80 || utf8.RuneCountInString(version) > 40 ||
		strings.ContainsAny(name, " \t\r\n") || strings.ContainsAny(version, " \t\r\n") ||
		!validPluginLibraryPath(libraryPath) {
		fail(c, 422, "library_path must be a .so file")
		return
	}
	if len(in.ConfigSchema) == 0 {
		in.ConfigSchema = []byte("{}")
	}
	if !validPluginSchema(in.ConfigSchema) {
		fail(c, 422, "config_schema must be an object")
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	ctx := c.Request.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		fail(c, 500, "create plugin failed")
		return
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO plugin_versions(name,version,library_path,config_schema,enabled)
        VALUES($1,$2,$3,$4,$5) RETURNING id`, name, version, libraryPath, in.ConfigSchema, enabled).Scan(&id)
	if isUniqueViolation(err) {
		fail(c, 409, "plugin version already exists")
		return
	}
	if err != nil {
		fail(c, 500, "create plugin failed")
		return
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'plugin.create','plugin_version',$2)", currentUser(c).ID, id); err != nil {
		fail(c, 500, "create plugin failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		fail(c, 500, "create plugin failed")
		return
	}
	c.JSON(201, gin.H{"id": id, "name": name, "version": version, "enabled": enabled})
}

type pluginUpdateInput struct {
	Enabled      *bool           `json:"enabled"`
	LibraryPath  *string         `json:"library_path"`
	ConfigSchema json.RawMessage `json:"config_schema"`
}

func (s *Server) updatePlugin(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in pluginUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	if in.Enabled == nil && in.LibraryPath == nil && len(in.ConfigSchema) == 0 {
		fail(c, 422, "no plugin changes provided")
		return
	}
	if len(in.ConfigSchema) > 0 && !validPluginSchema(in.ConfigSchema) {
		fail(c, 422, "config_schema must be an object")
		return
	}
	requestedPath := ""
	if in.LibraryPath != nil {
		requestedPath = strings.TrimSpace(*in.LibraryPath)
		if !validPluginLibraryPath(requestedPath) {
			fail(c, 422, "library_path must be a .so file")
			return
		}
	}
	ctx := c.Request.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		fail(c, 500, "update plugin failed")
		return
	}
	defer tx.Rollback(ctx)
	var enabled bool
	var schema json.RawMessage
	var libraryPath string
	err = tx.QueryRow(ctx, "SELECT enabled,config_schema,library_path FROM plugin_versions WHERE id=$1 FOR UPDATE", id).Scan(&enabled, &schema, &libraryPath)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "plugin not found")
		return
	}
	if err != nil {
		fail(c, 500, "update plugin failed")
		return
	}
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.LibraryPath != nil {
		libraryPath = requestedPath
	}
	if len(in.ConfigSchema) > 0 {
		schema = in.ConfigSchema
	}
	if _, err = tx.Exec(ctx, "UPDATE plugin_versions SET enabled=$2,library_path=$3,config_schema=$4 WHERE id=$1", id, enabled, libraryPath, schema); err != nil {
		fail(c, 500, "update plugin failed")
		return
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,action,target_type,target_id) VALUES($1,'plugin.update','plugin_version',$2)", currentUser(c).ID, id); err != nil {
		fail(c, 500, "update plugin failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		fail(c, 500, "update plugin failed")
		return
	}
	c.JSON(200, gin.H{"id": id, "enabled": enabled, "library_path": libraryPath})
}
