package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

//go:embed web/templates/* web/static/*
var assets embed.FS

var sessions = struct {
	sync.Mutex
	items map[string]time.Time
}{items: make(map[string]time.Time)}

func main() {
	dbPath := getenv("DATABASE_PATH", "/app/data/miniuptime.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil { log.Fatal(err) }
	db, err := sql.Open("sqlite", dbPath); if err != nil { log.Fatal(err) }; defer db.Close()
	if err := migrate(db); err != nil { log.Fatal(err) }
	go monitorLoop(db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", requireAuth(db, events))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		if err := db.Ping(); err != nil { http.Error(w, "database unavailable", 503); return }
		w.Header().Set("Content-Type", "application/json"); _, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	staticFS, _ := fs.Sub(assets, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { if configured(db) { http.Redirect(w, r, "/login", 302) } else { http.Redirect(w, r, "/setup", 302) } })
	mux.HandleFunc("GET /setup", setupPage(db)); mux.Handle("POST /setup", csrf(setupSubmit(db)))
	mux.HandleFunc("GET /login", loginPage(db)); mux.Handle("POST /login", csrf(loginSubmit(db)))
	mux.Handle("POST /logout", csrf(requireAuth(db, logout(db))))
	mux.HandleFunc("GET /dashboard", requireAuth(db, dashboard(db)))
	mux.HandleFunc("GET /groups", requireAuth(db, groupsPage(db)))
	mux.Handle("POST /groups", csrf(requireAuth(db, groupCreate(db))))
	mux.Handle("POST /groups/{id}/delete", csrf(requireAuth(db, groupDelete(db))))
	mux.HandleFunc("GET /monitors", requireAuth(db, monitorsPage(db)))
	mux.HandleFunc("GET /monitors/{id}", requireAuth(db, monitorDetail(db)))
	mux.HandleFunc("GET /incidents", requireAuth(db, incidentsPage(db)))
	mux.HandleFunc("GET /monitors/new", requireAuth(db, monitorForm))
	mux.HandleFunc("GET /monitors/{id}/edit", requireAuth(db, monitorEditPage(db)))
	mux.Handle("POST /monitors", csrf(requireAuth(db, monitorCreate(db))))
	mux.Handle("POST /monitors/{id}", csrf(requireAuth(db, monitorUpdate(db))))
	mux.Handle("POST /monitors/{id}/group", csrf(requireAuth(db, monitorAssignGroup(db))))
	mux.Handle("POST /monitors/{id}/toggle", csrf(requireAuth(db, monitorToggle(db))))
	mux.Handle("POST /monitors/{id}/delete", csrf(requireAuth(db, monitorDelete(db))))

	server := &http.Server{Addr: ":" + getenv("PORT", "3000"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { log.Printf("MiniUptime listening on %s", server.Addr); if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { log.Fatal(err) } }()
	ctx, stop := signalContext(); defer stop(); <-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel(); _ = server.Shutdown(shutdown)
}

func migrate(db *sql.DB) error { if _,err:=db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS admins(id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS groups(id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS monitors(id INTEGER PRIMARY KEY, group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL, name TEXT NOT NULL, type TEXT NOT NULL CHECK(type IN ('http','tcp','ping')), target TEXT NOT NULL, interval_seconds INTEGER NOT NULL DEFAULT 60, enabled INTEGER NOT NULL DEFAULT 1, current_status TEXT NOT NULL DEFAULT 'unknown', last_latency_ms INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', checked_at TEXT, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY, admin_id INTEGER NOT NULL, expires_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS checks(id INTEGER PRIMARY KEY, monitor_id INTEGER NOT NULL, status TEXT NOT NULL, latency_ms INTEGER NOT NULL, error TEXT, checked_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS incidents(id INTEGER PRIMARY KEY, monitor_id INTEGER NOT NULL, started_at TEXT NOT NULL, ended_at TEXT, error TEXT NOT NULL); INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (2, CURRENT_TIMESTAMP);`);err!=nil{return err}; var hasGroup bool; rows,err:=db.Query("PRAGMA table_info(monitors)");if err!=nil{return err};defer rows.Close();for rows.Next(){var cid int;var name,typ string;var notnull,pk int;var def any;if err:=rows.Scan(&cid,&name,&typ,&notnull,&def,&pk);err!=nil{return err};if name=="group_id"{hasGroup=true}};if !hasGroup {if _,err=db.Exec("ALTER TABLE monitors ADD COLUMN group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL");err!=nil{return err}};for _,column:=range []string{"current_status TEXT NOT NULL DEFAULT 'unknown'","last_latency_ms INTEGER NOT NULL DEFAULT 0","last_error TEXT NOT NULL DEFAULT ''","checked_at TEXT"} {name:=strings.Split(column," ")[0];var exists bool;rows2,_:=db.Query("PRAGMA table_info(monitors)");for rows2.Next(){var cid int;var n,t string;var nn,pk int;var d any;_=rows2.Scan(&cid,&n,&t,&nn,&d,&pk);if n==name{exists=true}};rows2.Close();if !exists {if _,err=db.Exec("ALTER TABLE monitors ADD COLUMN "+column);err!=nil{return err}}};return nil }
func configured(db *sql.DB) bool { var n int; return db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&n) == nil && n > 0 }
func setupPage(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { if configured(db) { http.Redirect(w, r, "/login", 302); return }; render(w, "setup.html", csrfData(w, r)) } }
func setupSubmit(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { if configured(db) { http.Error(w, "already configured", 409); return }; u, p := strings.TrimSpace(r.FormValue("username")), r.FormValue("password"); if len(u) < 3 || len(p) < 12 { http.Error(w, "username minimum 3 characters; password minimum 12 characters", 400); return }; h, _ := hashPassword(p); if _, err := db.Exec("INSERT INTO admins(username,password_hash,created_at) VALUES(?,?,?)", u, h, time.Now().UTC().Format(time.RFC3339)); err != nil { http.Error(w, "unable to create administrator", 400); return }; http.Redirect(w, r, "/login", 303) } }
func loginPage(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { if !configured(db) { http.Redirect(w, r, "/setup", 302); return }; render(w, "login.html", csrfData(w, r)) } }
func loginSubmit(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { var h string; if db.QueryRow("SELECT password_hash FROM admins WHERE username=?", r.FormValue("username")).Scan(&h) != nil || !checkPassword(r.FormValue("password"), h) { http.Error(w, "invalid credentials", 401); return }; token := randomToken(); if _,err:=db.Exec("INSERT INTO sessions(token,admin_id,expires_at) SELECT ?,id,? FROM admins WHERE username=?",token,time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339),r.FormValue("username")); err!=nil { http.Error(w,"unable to create session",500); return }; http.SetCookie(w, &http.Cookie{Name:"session", Value:token, Path:"/", HttpOnly:true, SameSite:http.SameSiteLaxMode, Secure:r.TLS != nil, MaxAge:86400}); http.Redirect(w, r, "/dashboard", 303) } }
func requireAuth(db *sql.DB, next http.HandlerFunc) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { c, err := r.Cookie("session"); var expiry string; if err!=nil || db.QueryRow("SELECT expires_at FROM sessions WHERE token=?",cookieValue(c,err)).Scan(&expiry)!=nil { http.Redirect(w,r,"/login",302); return }; if t,e:=time.Parse(time.RFC3339,expiry); e!=nil || time.Now().After(t) { http.Redirect(w,r,"/login",302); return }; next(w,r) } }
func logout(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { if c, err := r.Cookie("session"); err == nil { _,_=db.Exec("DELETE FROM sessions WHERE token=?",c.Value) }; http.SetCookie(w, &http.Cookie{Name:"session", MaxAge:-1, Path:"/", HttpOnly:true}); http.Redirect(w, r, "/login", 303) } }
func events(w http.ResponseWriter, r *http.Request) { flusher,ok:=w.(http.Flusher);if !ok{http.Error(w,"stream unsupported",500);return};w.Header().Set("Content-Type","text/event-stream");w.Header().Set("Cache-Control","no-cache");w.Header().Set("Connection","keep-alive");ticker:=time.NewTicker(5*time.Second);defer ticker.Stop();for {select{case <-r.Context().Done():return;case <-ticker.C:fmt.Fprint(w,"event: status\\ndata: update\\n\\n");flusher.Flush()}} }
func dashboard(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { var total,up,down int; _=db.QueryRow("SELECT COUNT(*) FROM monitors").Scan(&total); _=db.QueryRow("SELECT COUNT(*) FROM monitors WHERE current_status='up'").Scan(&up); _=db.QueryRow("SELECT COUNT(*) FROM monitors WHERE current_status='down'").Scan(&down); rows,_:=db.Query("SELECT id,name,type,target,current_status,last_latency_ms FROM monitors ORDER BY name"); if rows==nil { http.Error(w,"unable to load monitors",500); return }; defer rows.Close();var monitors []map[string]any;for rows.Next(){var id,lat int;var name,typ,target,status string;_=rows.Scan(&id,&name,&typ,&target,&status,&lat);var checks,success int; _=db.QueryRow("SELECT COUNT(*),COALESCE(SUM(status='up'),0) FROM checks WHERE monitor_id=?",id).Scan(&checks,&success); uptime:=0;if checks>0{uptime=success*100/checks};monitors=append(monitors,map[string]any{"ID":id,"Name":name,"Type":typ,"Target":target,"Status":status,"Latency":lat,"Uptime":uptime})};ir,_:=db.Query("SELECT m.name,i.started_at,COALESCE(i.ended_at,'') FROM incidents i JOIN monitors m ON m.id=i.monitor_id ORDER BY i.id DESC LIMIT 5");defer ir.Close();var incidents []map[string]string;for ir.Next(){var name,started,ended string;_=ir.Scan(&name,&started,&ended);incidents=append(incidents,map[string]string{"Name":name,"Started":started,"Ended":ended})};render(w, "dashboard.html", map[string]any{"CSRF":csrfData(w,r)["CSRF"],"Total":total,"Up":up,"Down":down,"Monitors":monitors,"Incidents":incidents}) } }
type group struct { ID int; Name string; CSRF string }
func groupsPage(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { rows,err:=db.Query("SELECT id,name FROM groups ORDER BY name"); if err!=nil {http.Error(w,err.Error(),500);return}; defer rows.Close(); var list []group; for rows.Next(){var g group; if err:=rows.Scan(&g.ID,&g.Name);err!=nil{http.Error(w,err.Error(),500);return};list=append(list,g)}; token:=csrfData(w,r)["CSRF"];for i:=range list{list[i].CSRF=token};render(w,"groups.html",map[string]any{"Groups":list,"CSRF":token}) } }
func groupCreate(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) { name:=strings.TrimSpace(r.FormValue("name"));if name==""{http.Error(w,"name required",400);return};if _,err:=db.Exec("INSERT INTO groups(name,created_at) VALUES(?,?)",name,time.Now().UTC().Format(time.RFC3339));err!=nil{http.Error(w,"group already exists",409);return};http.Redirect(w,r,"/groups",303) } }
func groupDelete(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) {if _,err:=db.Exec("DELETE FROM groups WHERE id=?",r.PathValue("id"));err!=nil{http.Error(w,err.Error(),500);return};http.Redirect(w,r,"/groups",303)} }
type monitor struct { ID int; GroupID int; GroupName, Name, Type, Target string; Interval int; Enabled bool; CSRF string }
func monitorsPage(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { rows, err := db.Query("SELECT m.id,COALESCE(m.group_id,0),COALESCE(g.name,''),m.name,m.type,m.target,m.interval_seconds,m.enabled FROM monitors m LEFT JOIN groups g ON g.id=m.group_id ORDER BY m.id DESC"); if err != nil { http.Error(w, err.Error(), 500); return }; defer rows.Close(); var list []monitor; for rows.Next() { var m monitor; var enabled int; if err := rows.Scan(&m.ID,&m.GroupID,&m.GroupName,&m.Name,&m.Type,&m.Target,&m.Interval,&enabled); err != nil { http.Error(w, err.Error(), 500); return }; m.Enabled=enabled == 1; list=append(list,m) }; token := csrfData(w, r)["CSRF"]; for i := range list { list[i].CSRF = token }; render(w,"monitors.html",list) } }
func monitorForm(w http.ResponseWriter, r *http.Request) { render(w,"monitor-form.html",csrfData(w, r)) }
func monitorEditPage(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { var m monitor; var enabled int; if err:=db.QueryRow("SELECT id,COALESCE(group_id,0),name,type,target,interval_seconds,enabled FROM monitors WHERE id=?",r.PathValue("id")).Scan(&m.ID,&m.GroupID,&m.Name,&m.Type,&m.Target,&m.Interval,&enabled); err!=nil { http.NotFound(w,r); return }; m.Enabled=enabled==1; groups:=[]group{}; rows,_:=db.Query("SELECT id,name FROM groups ORDER BY name"); if rows!=nil { defer rows.Close(); for rows.Next(){var g group; _=rows.Scan(&g.ID,&g.Name); groups=append(groups,g)} }; data:=map[string]any{"Monitor":m,"Groups":groups,"CSRF":csrfData(w,r)["CSRF"]}; render(w,"monitor-edit.html",data) } }
func monitorAssignGroup(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) { groupID:=strings.TrimSpace(r.FormValue("group_id")); var err error; if groupID=="" { _,err=db.Exec("UPDATE monitors SET group_id=NULL WHERE id=?",r.PathValue("id")) } else { _,err=db.Exec("UPDATE monitors SET group_id=? WHERE id=?",groupID,r.PathValue("id")) }; if err!=nil {http.Error(w,err.Error(),400);return}; http.Redirect(w,r,"/monitors",303) } }
func monitorUpdate(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) { typ:=r.FormValue("type"); name,target:=strings.TrimSpace(r.FormValue("name")),strings.TrimSpace(r.FormValue("target")); interval:=60; if _, scanErr := fmt.Sscanf(r.FormValue("interval"), "%d", &interval); (typ!="http"&&typ!="tcp"&&typ!="ping") || name=="" || target=="" || scanErr!=nil || interval<10 { http.Error(w,"invalid monitor data",400); return }; groupID:=strings.TrimSpace(r.FormValue("group_id")); var err error; if groupID=="" { _,err=db.Exec("UPDATE monitors SET name=?,type=?,target=?,interval_seconds=?,group_id=NULL WHERE id=?",name,typ,target,interval,r.PathValue("id")) } else { _,err=db.Exec("UPDATE monitors SET name=?,type=?,target=?,interval_seconds=?,group_id=? WHERE id=?",name,typ,target,interval,groupID,r.PathValue("id")) }; if err!=nil {http.Error(w,err.Error(),500);return}; http.Redirect(w,r,"/monitors",303) } }
func csrfData(w http.ResponseWriter, r *http.Request) map[string]string { token:=""; if c,err:=r.Cookie("csrf"); err==nil { token=c.Value }; if token=="" { token=randomToken(); http.SetCookie(w,&http.Cookie{Name:"csrf",Value:token,Path:"/",HttpOnly:true,SameSite:http.SameSiteLaxMode,Secure:r.TLS!=nil,MaxAge:3600}) }; return map[string]string{"CSRF":token} }
func checkCSRF(r *http.Request) bool { c,err:=r.Cookie("csrf"); return err==nil && c.Value!="" && c.Value==r.FormValue("csrf") }
func csrf(next http.HandlerFunc) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) { if r.Method==http.MethodPost && !checkCSRF(r) { http.Error(w,"invalid csrf token",http.StatusForbidden); return }; next(w,r) } }
func monitorLoop(db *sql.DB) { jobs:=make(chan monitorJob,32); for i:=0;i<4;i++ { go func(){ for job:=range jobs { runCheck(db,job.id,job.typ,job.target) } }() }; ticker:=time.NewTicker(5*time.Second); defer ticker.Stop(); for range ticker.C { rows,err:=db.Query("SELECT m.id,m.type,m.target,m.interval_seconds,COALESCE(MAX(c.checked_at),'') FROM monitors m LEFT JOIN checks c ON c.monitor_id=m.id WHERE m.enabled=1 GROUP BY m.id"); if err!=nil {continue}; now:=time.Now(); for rows.Next(){var id,interval int;var typ,target,last string;if rows.Scan(&id,&typ,&target,&interval,&last)!=nil{continue}; if last=="" {jobs<-monitorJob{id,typ,target};continue}; checked,e:=time.Parse(time.RFC3339,last);if e==nil&&now.Sub(checked)>=time.Duration(interval)*time.Second {jobs<-monitorJob{id,typ,target}} }; rows.Close() } }
type monitorJob struct{id int;typ,target string}
func runCheck(db *sql.DB,id int,typ,target string) { started:=time.Now(); var err error; for attempt:=0; attempt<3; attempt++ { err=checkTarget(typ,target); if err==nil || attempt==2 { break }; time.Sleep(time.Duration(attempt+1)*500*time.Millisecond) }; status,message:="up",""; if err!=nil { status="down"; message=err.Error() }; latency:=time.Since(started).Milliseconds(); now:=time.Now().UTC().Format(time.RFC3339); var previous string; _=db.QueryRow("SELECT current_status FROM monitors WHERE id=?",id).Scan(&previous); _,_=db.Exec("INSERT INTO checks(monitor_id,status,latency_ms,error,checked_at) VALUES(?,?,?,?,?)",id,status,latency,message,now); if status=="down" && previous!="down" { _,_=db.Exec("INSERT INTO incidents(monitor_id,started_at,error) VALUES(?,?,?)",id,now,message) }; if status=="up" && previous=="down" { _,_=db.Exec("UPDATE incidents SET ended_at=? WHERE monitor_id=? AND ended_at IS NULL",now,id) }; _,_=db.Exec("UPDATE monitors SET current_status=?,last_latency_ms=?,last_error=?,checked_at=? WHERE id=?",status,latency,message,now,id) }
func checkTarget(typ, target string) error { ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second); defer cancel(); switch typ { case "http": req,err:=http.NewRequestWithContext(ctx,http.MethodGet,target,nil); if err!=nil{return err}; resp,err:=http.DefaultClient.Do(req); if err==nil{defer resp.Body.Close();if resp.StatusCode>=400{return fmt.Errorf("HTTP %s",resp.Status)}};return err; case "tcp": conn,err:=net.DialTimeout("tcp",target,10*time.Second);if err==nil{conn.Close()};return err; case "ping":return exec.CommandContext(ctx,"ping","-c","1","-W","5",target).Run() };return fmt.Errorf("unknown monitor type") }
func incidentsPage(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) { rows,err:=db.Query("SELECT i.started_at,COALESCE(i.ended_at,''),m.name,i.error FROM incidents i JOIN monitors m ON m.id=i.monitor_id ORDER BY i.id DESC LIMIT 100");if err!=nil{http.Error(w,err.Error(),500);return};defer rows.Close();var incidents []map[string]string;for rows.Next(){var started,ended,name,e string;_=rows.Scan(&started,&ended,&name,&e);incidents=append(incidents,map[string]string{"Started":started,"Ended":ended,"Monitor":name,"Error":e})};render(w,"incidents.html",incidents) } }
func monitorDetail(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) { var m monitor; var enabled int; if err:=db.QueryRow("SELECT id,COALESCE(group_id,0),name,type,target,interval_seconds,enabled FROM monitors WHERE id=?",r.PathValue("id")).Scan(&m.ID,&m.GroupID,&m.Name,&m.Type,&m.Target,&m.Interval,&enabled);err!=nil{http.NotFound(w,r);return}; rows,err:=db.Query("SELECT status,latency_ms,COALESCE(error,''),checked_at FROM checks WHERE monitor_id=? ORDER BY id DESC LIMIT 50",m.ID);if err!=nil{http.Error(w,err.Error(),500);return};defer rows.Close();var checks []map[string]any;for rows.Next(){var s,e,at string;var l int;_=rows.Scan(&s,&l,&e,&at);checks=append(checks,map[string]any{"Status":s,"Latency":l,"Error":e,"At":at})};render(w,"monitor-detail.html",map[string]any{"Monitor":m,"Checks":checks}) } }
func validMonitor(typ, name, target string, interval int) bool { target=strings.TrimSpace(target); if typ=="http" {u,e:=url.ParseRequestURI(target);if e!=nil||u.Scheme!="http"&&u.Scheme!="https"||u.Host==""{return false}} else if typ=="tcp" {if _,_,e:=net.SplitHostPort(target);e!=nil{return false}} else if typ=="ping" {if strings.Contains(target,"://")||target==""{return false}} else{return false};return strings.TrimSpace(name)!=""&&interval>=10 }
func monitorCreate(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) { typ:=r.FormValue("type"); if typ!="http" && typ!="tcp" && typ!="ping" { http.Error(w,"invalid monitor type",400); return }; name,target:=strings.TrimSpace(r.FormValue("name")),strings.TrimSpace(r.FormValue("target")); if name=="" || target=="" { http.Error(w,"name and target required",400); return }; interval:=60; if _,err:=fmt.Sscanf(r.FormValue("interval"),"%d",&interval);err!=nil || !validMonitor(typ,name,target,interval) { http.Error(w,"interval must be at least 10 seconds",400); return }; _,err:=db.Exec("INSERT INTO monitors(name,type,target,interval_seconds,created_at) VALUES(?,?,?,?,?)",name,typ,target,interval,time.Now().UTC().Format(time.RFC3339)); if err!=nil {http.Error(w,err.Error(),500);return}; http.Redirect(w,r,"/monitors",303) } }
func monitorToggle(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) { _,err:=db.Exec("UPDATE monitors SET enabled=CASE enabled WHEN 1 THEN 0 ELSE 1 END WHERE id=?",r.PathValue("id")); if err!=nil {http.Error(w,err.Error(),500);return}; http.Redirect(w,r,"/monitors",303) } }
func monitorDelete(db *sql.DB) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request) { _,err:=db.Exec("DELETE FROM monitors WHERE id=?",r.PathValue("id")); if err!=nil {http.Error(w,err.Error(),500);return}; http.Redirect(w,r,"/monitors",303) } }
func render(w http.ResponseWriter, name string, data any) { t, err := template.ParseFS(assets, "web/templates/"+name); if err != nil { http.Error(w, err.Error(), 500); return }; _ = t.Execute(w, data) }
func hashPassword(p string) (string, error) { salt:=make([]byte,16); if _,err:=rand.Read(salt);err!=nil{return "",err}; return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%x$%x",salt,argon2.IDKey([]byte(p),salt,3,64*1024,2,32)),nil }
func checkPassword(p,h string) bool { var salt, expected []byte; if _,err:=fmt.Sscanf(h,"$argon2id$v=19$m=65536,t=3,p=2$%x$%x",&salt,&expected);err!=nil{return false}; got:=argon2.IDKey([]byte(p),salt,3,64*1024,2,32); return string(got)==string(expected) }
func randomToken() string { b:=make([]byte,32); _,_=rand.Read(b); return fmt.Sprintf("%x",b) }
func cookieValue(c *http.Cookie, err error) string { if err != nil { return "" }; return c.Value }
func getenv(k,f string) string { if v:=os.Getenv(k);v!=""{return v};return f }
func signalContext() (context.Context, context.CancelFunc) { return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) }
