package passkeys

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/go-webauthn/webauthn/webauthn"
)

type User struct {
	ID          []byte
	Name        string
	DisplayName string
	Creds       []webauthn.Credential
}

func (u *User) WebAuthnID() []byte                         { return u.ID }
func (u *User) WebAuthnName() string                       { return u.Name }
func (u *User) WebAuthnDisplayName() string                { return u.DisplayName }
func (u *User) WebAuthnIcon() string                       { return "" }
func (u *User) WebAuthnCredentials() []webauthn.Credential { return u.Creds }

var (
	appW    *webauthn.WebAuthn
	users   = map[string]*User{} // email -> user move to blog?
	usrLock sync.Mutex
	sessReg = map[string]*webauthn.SessionData{} // email -> session (registration)
	sessLog = map[string]*webauthn.SessionData{} // email -> session (login)
)

func Mux() http.Handler {
	cfg := &webauthn.Config{
		RPDisplayName: "Example Passkeys",
		RPID:          "localhost",                       // set to your registrable domain in prod (e.g., example.com)
		RPOrigins:     []string{"http://localhost:8080"}, // must match your origin
	}
	var err error
	appW, err = webauthn.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	// Demo: single “account” identified by email query param
	mux.HandleFunc("/register/begin", handleRegisterBegin) // POST email
	mux.HandleFunc("/register/finish", handleRegisterFinish)
	mux.HandleFunc("/login/begin", handleLoginBegin) // POST email
	mux.HandleFunc("/login/finish", handleLoginFinish)
	mux.HandleFunc("/me", handleMe)

	return mux
}

type emailReq struct {
	Email string `json:"email"`
}

func getOrCreateUser(email string) *User {
	usrLock.Lock()
	defer usrLock.Unlock()
	u := users[email]
	if u == nil {
		u = &User{
			ID:          []byte(email), // simple demo; in prod use stable random bytes
			Name:        email,
			DisplayName: email,
			Creds:       []webauthn.Credential{},
		}
		users[email] = u
	}
	return u
}

func handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	var req emailReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}
	user := getOrCreateUser(req.Email)

	/*opts := webauthn.RegistrationOption{
		AuthenticatorSelection: webauthn.AuthenticatorSelection{
			UserVerification: webauthn.UserVerificationPreferred,
		},
	}*/

	creation, sessionData, err := appW.BeginRegistration(user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessReg[req.Email] = sessionData
	writeJSON(w, creation)
}

func handleRegisterFinish(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}
	user := getOrCreateUser(email)
	session, ok := sessReg[email]
	if !ok {
		http.Error(w, "no registration session", http.StatusBadRequest)
		return
	}
	cred, err := appW.FinishRegistration(user, *session, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	usrLock.Lock()
	user.Creds = append(user.Creds, *cred)
	usrLock.Unlock()
	delete(sessReg, email)
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: email, Path: "/", HttpOnly: true})
	writeJSON(w, map[string]any{"registered": true})
}

func handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	var req emailReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}
	user := getOrCreateUser(req.Email)

	requestOptions, sessionData, err := appW.BeginLogin(user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessLog[req.Email] = sessionData
	writeJSON(w, requestOptions)
}

func handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}
	user := getOrCreateUser(email)
	session, ok := sessLog[email]
	if !ok {
		http.Error(w, "no login session", http.StatusBadRequest)
		return
	}
	_, err := appW.FinishLogin(user, *session, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	delete(sessLog, email)
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: email, Path: "/", HttpOnly: true})
	writeJSON(w, map[string]any{"logged_in": true})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie("sid")
	if c == nil || users[c.Value] == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]string{"email": c.Value})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
