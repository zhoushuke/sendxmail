package http

import (
	"bytes"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/zhoushuke/sendxmail/g"
	"github.com/jordan-wright/email"
)

const (
	TPL_FILE   = "template/mail.template.tmpl"
	TPL_LAYOUT = "layout"
)

type HTMLBODY struct {
	Title    string
	Logo     string
	LogoLink string
	Kvs      map[string]string
}

func configSmtpRoutes() {
	http.HandleFunc("/api/mail", func(w http.ResponseWriter, r *http.Request) {
		SendMailBySmtp(w, r, false)
	})
	http.HandleFunc("/api/attachmail", func(w http.ResponseWriter, r *http.Request) {
		SendMailBySmtp(w, r, true)
	})
}

func templateRender(body string) (buf bytes.Buffer, err error) {
	// get 消息体, field以, 分隔, k-v以|分隔
	// "event_time|2020-04-11,host_ip|127.0.0.1"
	b1 := strings.Split(body, g.Config().Style.FieldSplit)
	_len := len(b1)
	h := HTMLBODY{}
	h.Kvs = make(map[string]string)
	h.Title = g.Config().Style.Title
	h.Logo = g.Config().Style.Logo
	h.LogoLink = g.Config().Style.LogoLink
	// 获取k-v body
	for i := 0; i < _len; i++ {
		kv := strings.Split(b1[i], g.Config().Style.KvSplit)
		h.Kvs[kv[0]] = kv[1]
	}
	log.Println("[INFO]", "body:", h)
	// 模板渲染
	t, err := template.ParseFiles(TPL_FILE)
	if err != nil {
		log.Println("[ERROR]", "parse file err", err)
		return buf, err
	}

	if err := t.ExecuteTemplate(&buf, TPL_LAYOUT, h); err != nil {
		log.Println("[ERROR]", "There was an error", err.Error())
		return buf, err
	}
	return buf, nil
}

func SendMailBySmtp(w http.ResponseWriter, r *http.Request, hasAttach bool) {
	var (
		err    error
		server string
		user   string
		passwd string
	)

	addr := strings.Split(r.RemoteAddr, ":")[0]
	allowList := strings.Split(g.Config().Http.WhiteList, ",")
	authorized := isValid(addr, allowList)

	//授权检查
	if authorized == false {
		http.Error(w, "remote not in whitelist", http.StatusBadRequest)
		return
	}

	if r.ContentLength == 0 {
		http.Error(w, "body is blank", http.StatusBadRequest)
		return
	}

	//根据是否有附件，使用不同的ParseForm方法
	if hasAttach == true {
		err = r.ParseMultipartForm(g.Config().Smtp.MaxBytes)
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		http.Error(w, "param prase error", http.StatusBadRequest)
		return
	}

	if len(r.Form["tos"]) == 0 || len(r.Form["content"]) == 0 || len(r.Form["subject"]) == 0 {
		http.Error(w, "param tos,content or subject lost", http.StatusBadRequest)
		return
	}

	e := email.NewEmail()
	//收件人
	e.To = strings.Split(r.Form.Get("tos"), g.Config().Smtp.Spliter)
	//主题
	e.Subject = r.Form.Get("subject")

	//抄送，可选
	if len(r.Form.Get("cc")) != 0 {
		e.Cc = strings.Split(r.Form.Get("cc"), g.Config().Smtp.Spliter)
	}

	//密送，可选
	if len(r.Form.Get("bcc")) != 0 {
		e.Bcc = strings.Split(r.Form.Get("bcc"), g.Config().Smtp.Spliter)
	}
	// fmt.Println("[INFO]: format", r.Form.Get("format"))
	//消息体格式，可选, 默认为text，如果format设置为html，则以html方式发送, 如果format设置为template, 则使用模板渲染方式.
	// html
	if len(r.Form.Get("format")) != 0 && r.Form.Get("format") == "html" {
		e.HTML = []byte(r.Form.Get("content"))
		// template
	} else if len(r.Form.Get("format")) != 0 && r.Form.Get("format") == "template" {
		// 是否设置邮件title
		if len(r.Form.Get("title")) != 0 {
			g.Config().Style.Title = r.Form.Get("title")
		}

		// 是否设置邮件logo
		if len(r.Form.Get("logo")) != 0 {
			g.Config().Style.Logo = r.Form.Get("logo")
		}

		// 是否设置邮件logolink
		if len(r.Form.Get("logolink")) != 0 {
			g.Config().Style.LogoLink = r.Form.Get("logolink")
		}

		// get 消息体, field以, 分隔, k-v以|分隔
		// "event_time|2020-04-11,host_ip|127.0.0.1"
		body := r.Form.Get("content")
		buf, err := templateRender(body)
		if err != nil {
			log.Println("[ERROR]", "template render error", err)
			return
		}
		// fmt.Println(buf.String())
		e.HTML = []byte(buf.String())
		// text
	} else {
		e.Text = []byte(r.Form.Get("content"))
	}

	//附件，可选
	if hasAttach == true {
		n, err := strconv.Atoi(r.Form.Get("attachNum"))
		if err != nil {
			http.Error(w, "attachNum must exists and bigger than 0", http.StatusBadRequest)
			return
		}

		m := r.MultipartForm

		for i := 1; i <= n; i++ {
			//get the *fileheaders
			files := m.File["attach"+strconv.Itoa(i)]
			for i, _ := range files {
				//for each fileheader, get a handle to the actual file
				file, err := files[i].Open()
				defer file.Close()
				if err != nil {
					http.Error(w, "open attach file stream error", http.StatusInternalServerError)
					return
				}
				//attach each file to email
				e.Attach(file, files[i].Filename, "")
			}
		}

	}

	//smtp服务器，如果没有则使用默认配置
	if len(r.Form.Get("server")) != 0 && len(r.Form.Get("user")) != 0 && len(r.Form.Get("passwd")) != 0 {
		server = r.Form.Get("server")
		user = r.Form.Get("user")
		passwd = r.Form.Get("passwd")
	} else {
		server = g.Config().Smtp.Addr
		user = g.Config().Smtp.User
		passwd = g.Config().Smtp.Pass
	}

	//发件人，可以设置成 San Zhang <zhangsan@example.com> 这种形式(注意不能有非ascii字符)，如果不设置，默认为登陆使用的账号
	if len(r.Form.Get("from")) != 0 {
		e.From = r.Form.Get("from")
	} else {
		e.From = user
	}

	hp := strings.Split(server, ":")
	auth := smtp.PlainAuth("", user, passwd, hp[0])
	//暂时不支持TLS/StartTLS等加密认证
	error := e.Send(server, auth)
	if error != nil {
		log.Println("[ERROR]", addr, e.From, e.To, e.Subject, error)
		http.Error(w, error.Error(), http.StatusBadRequest)
		return
	}
	log.Println("[INFO]", addr, e.From, e.To, e.Subject)
	w.Write([]byte("{\"status\":0,\"msg\":\"ok\"}"))
	return
}

//检查调用IP合法性函数
func isValid(Addr string, allowList []string) (authorized bool) {
	rAddr := net.ParseIP(Addr)
	for v := range allowList {
		_, ipNet, err := net.ParseCIDR(allowList[v])
		if err != nil {
			//log.Println("parse ip net error")
			ipHost := net.ParseIP(allowList[v])
			if ipHost != nil {
				if ipHost.Equal(rAddr) {
					authorized = true
				}
			} else {
				log.Fatalln("ip list error")
			}
		} else {
			//log.Println("Contains ip ")
			if ipNet.Contains(rAddr) {
				authorized = true
			}
		}
	}
	return authorized
}
