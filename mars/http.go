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
	writeIDs(w, r, false)
}

func GetID53(w http.ResponseWriter, r *http.Request) {
	writeIDs(w, r, true)
}

func writeIDs(w http.ResponseWriter, r *http.Request, short bool) {
	num := r.URL.Query().Get("num")
	count := 1
	if num != "" {
		var err error
		count, err = strconv.Atoi(num)
		if err != nil || count < 1 || count > maxNum {
			http.Error(w, fmt.Sprintf("请求数量必须为1到%d", maxNum), http.StatusBadRequest)
			return
		}
	}
	ids, err := m.generateIDs(count, short)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	for _, id := range ids {
		if num == "" {
			fmt.Fprint(w, id)
		} else {
			fmt.Fprintln(w, id)
		}
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

func GetSeq(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if s, ok := m.seqMap[vars["id"]]; ok {
		num := r.URL.Query().Get("num")
		if num == "" {
			next, err := s.Next()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, err.Error())
				return
			}
			fmt.Fprint(w, next)
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
			next, err := s.Next()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, err.Error())
				return
			}
			fmt.Fprintln(w, next)
		}

	} else {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "sequence id "+vars["id"]+" not exist")
	}
}
