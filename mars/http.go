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
	ids := m.gen.GetStr()
	//ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	//_, err := etcdsrv.New().Cli.Put(ctx, "mars/keys/"+ids, ids)
	//cancel()
	//if err != nil {
	//	w.WriteHeader(http.StatusInternalServerError)
	//	fmt.Fprintf(w, "Server Error,%v", err)
	//	return
	//}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "%s", ids)
}

func GetIDInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	i, err := strconv.Atoi(vars["id"])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "%s 不是合法的id", vars["id"])
		return
	}
	response, err := m.etcdCli.Get(context.Background(), workerKey+"/"+strconv.FormatInt(m.gen.GetNode(int64(i)), 10))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, err)
		return
	}
	res := make(map[string]string)
	res["time"] = m.gen.GetTime(int64(i))
	res["step"] = strconv.FormatInt(m.gen.GetStep(int64(i)), 10)
	res["node"] = string(response.Kvs[0].Value)
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
