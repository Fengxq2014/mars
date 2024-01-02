package mars

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"net/http"
	"strconv"
)

var maxNum = 100

type authenticationMiddleware struct {
	tokenUsers map[string]string
}

func (amw *authenticationMiddleware) Populate(name, pwd string) {
	amw.tokenUsers = make(map[string]string)
	amw.tokenUsers[name] = authorizationHeader(name, pwd)
}

func (amw *authenticationMiddleware) Check(authValue string) (string, bool) {
	if authValue == "" {
		return "", false
	}
	for i, v := range amw.tokenUsers {
		if v == authValue {
			return i, true
		}
	}
	return "", false
}

func (amw *authenticationMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if user, found := amw.Check(auth); found {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), "user", user)))
		} else {
			http.Error(w, "Forbidden", http.StatusForbidden)
		}
	})
}

func authorizationHeader(user, password string) string {
	base := user + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(base))
}

func GetID(w http.ResponseWriter, r *http.Request) {
	num := r.URL.Query().Get("num")
	if num == "" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%s", m.gen.GetStr())
		return
	}
	atoi, err := strconv.Atoi(num)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "错误的参数内容%s", num)
		return
	}
	if atoi > maxNum {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "请求数量:%d,超出最大限制数量%d", atoi, maxNum)
		return
	}
	for i := 0; i < atoi; i++ {
		fmt.Fprintln(w, m.gen.GetID())
	}
	//w.WriteHeader(http.StatusOK)

	//ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	//_, err := etcdsrv.New().Cli.Put(ctx, "mars/keys/"+ids, ids)
	//cancel()
	//if err != nil {
	//	w.WriteHeader(http.StatusInternalServerError)
	//	fmt.Fprintf(w, "Server Error,%v", err)
	//	return
	//}
}

func GetID53(w http.ResponseWriter, r *http.Request) {
	num := r.URL.Query().Get("num")
	if num == "" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%s", m.gen.GetStr53())
		return
	}
	atoi, err := strconv.Atoi(num)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "错误的参数内容%s", num)
		return
	}
	if atoi > maxNum {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "请求数量:%d,超出最大限制数量%d", atoi, maxNum)
		return
	}
	for i := 0; i < atoi; i++ {
		fmt.Fprintln(w, m.gen.GetID53())
	}
}

func GetIDInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	info, err := m.getIdInfo(vars["id"])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, err)
		return
	}
	res := make(map[string]string)
	res["time"] = info.Time
	res["step"] = info.Step
	res["node"] = info.Node
	bytes, err := json.Marshal(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}
