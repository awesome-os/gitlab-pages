package httperrors

import (
	"fmt"
	"net/http"

	"gitlab.com/gitlab-org/gitlab-pages/internal/errortracking"
	"gitlab.com/gitlab-org/gitlab-pages/internal/logging"
)

type content struct {
	status       int
	title        string
	statusString string
	header       string
	subHeader    string
}

// genericErrorHeader and genericErrorSubHeader are the shared copy for the
// 500/502/503 server-error pages.
const (
	genericErrorHeader    = "Whoops, something went wrong on our end."
	genericErrorSubHeader = `<p>Try refreshing the page, or going back and attempting the action again.</p>
     <p>Please contact your GitLab administrator if this problem persists.</p>`
)

var (
	content401 = content{
		http.StatusUnauthorized,
		"Unauthorized (401)",
		"401",
		"You don't have permission to access the resource.",
		`<p>The resource that you are attempting to access is protected and you don't have the necessary permissions to view it.</p>`,
	}
	content404 = content{
		http.StatusNotFound,
		"The page you're looking for could not be found (404)",
		"404",
		"The page you're looking for could not be found.",
		`<p>The resource that you are attempting to access does not exist or you don't have the necessary permissions to view it.</p>
     <p>Make sure the address is correct and that the page hasn't moved.</p>
     <p>Please contact your GitLab administrator if you think this is a mistake.</p>`,
	}
	content414 = content{
		status:       http.StatusRequestURITooLong,
		title:        "Request URI Too Long (414)",
		statusString: "414",
		header:       "Request URI Too Long.",
		subHeader: `<p>The URI provided was too long for the server to process.</p>
			<p>Try to make the request URI shorter.</p>`,
	}

	content429 = content{
		http.StatusTooManyRequests,
		"Too many requests (429)",
		"429",
		"Too many requests.",
		`<p>The resource that you are attempting to access is being rate limited.</p>`,
	}
	content500 = content{
		http.StatusInternalServerError,
		"Something went wrong (500)",
		"500",
		genericErrorHeader,
		genericErrorSubHeader,
	}

	content502 = content{
		http.StatusBadGateway,
		"Something went wrong (502)",
		"502",
		genericErrorHeader,
		genericErrorSubHeader,
	}

	content503 = content{
		http.StatusServiceUnavailable,
		"Service Unavailable (503)",
		"503",
		genericErrorHeader,
		genericErrorSubHeader,
	}
)

const predefinedErrorPage = `
<!DOCTYPE html>
<html>
<head>
  <meta content="width=device-width, initial-scale=1, maximum-scale=1" name="viewport">
  <title>%v</title>
  <style>
    :root {
      --background: white;
      --border: #dcdcde;
      --color: #3a383f;
      --subtle: #626168;
      --link: #0b5cad;

      background-color: var(--background);
      color: var(--color);
    }

    body {
      text-align: center;
      font-family: "GitLab Sans", -apple-system, BlinkMacSystemFont,
        "Segoe UI", Roboto, "Noto Sans", Ubuntu, Cantarell, "Helvetica Neue",
        sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Segoe UI Symbol",
        "Noto Color Emoji";
      margin: auto;
      font-size: 14px;
    }

    h1 {
      font-size: 56px;
      line-height: 100px;
      font-weight: 400;
      color: var(--subtle);
    }

    h2 {
      color: var(--subtle);
      font-size: 20px;
      font-weight: 400;
      line-height: 28px;
    }

    hr {
      max-width: 800px;
      margin: 18px auto;
      border: 0;
      border-top: 1px solid var(--border);
      border-bottom: 1px solid var(--background);
    }

    img {
      max-width: 40vw;
      display: block;
      margin: 40px auto;
    }

    a {
      line-height: 100px;
      font-weight: 400;
      color: var(--link);
      font-size: 18px;
      text-decoration: none;
    }

    .container {
      margin: auto 20px;
    }

    .go-back {
      display: none;
    }

    @media (prefers-color-scheme: dark) {
      :root {
        --background: #18171d;
        --border: #4c4b51;
        --color: #ececef;
        --subtle: #bfbfc3;
        --link: #9dc7f1;
      }
    }
  </style>
</head>

<body>
  <img src="data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjAwIiBoZWlnaHQ9IjE5MiIgdmlld0JveD0iMCAwIDI1IDI0IiBmaWxsPSJub25lIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPgogIDxwYXRoIGQ9Im0yNC41MDcgOS41LS4wMzQtLjA5TDIxLjA4Mi41NjJhLjg5Ni44OTYgMCAwIDAtMS42OTQuMDkxbC0yLjI5IDcuMDFINy44MjVMNS41MzUuNjUzYS44OTguODk4IDAgMCAwLTEuNjk0LS4wOUwuNDUxIDkuNDExLjQxNiA5LjVhNi4yOTcgNi4yOTcgMCAwIDAgMi4wOSA3LjI3OGwuMDEyLjAxLjAzLjAyMiA1LjE2IDMuODY3IDIuNTYgMS45MzUgMS41NTQgMS4xNzZhMS4wNTEgMS4wNTEgMCAwIDAgMS4yNjggMGwxLjU1NS0xLjE3NiAyLjU2LTEuOTM1IDUuMTk3LTMuODkuMDE0LS4wMUE2LjI5NyA2LjI5NyAwIDAgMCAyNC41MDcgOS41WiIKICAgICAgICBmaWxsPSIjRTI0MzI5Ii8+CiAgPHBhdGggZD0ibTI0LjUwNyA5LjUtLjAzNC0uMDlhMTEuNDQgMTEuNDQgMCAwIDAtNC41NiAyLjA1MWwtNy40NDcgNS42MzIgNC43NDIgMy41ODQgNS4xOTctMy44OS4wMTQtLjAxQTYuMjk3IDYuMjk3IDAgMCAwIDI0LjUwNyA5LjVaIgogICAgICAgIGZpbGw9IiNGQzZEMjYiLz4KICA8cGF0aCBkPSJtNy43MDcgMjAuNjc3IDIuNTYgMS45MzUgMS41NTUgMS4xNzZhMS4wNTEgMS4wNTEgMCAwIDAgMS4yNjggMGwxLjU1NS0xLjE3NiAyLjU2LTEuOTM1LTQuNzQzLTMuNTg0LTQuNzU1IDMuNTg0WiIKICAgICAgICBmaWxsPSIjRkNBMzI2Ii8+CiAgPHBhdGggZD0iTTUuMDEgMTEuNDYxYTExLjQzIDExLjQzIDAgMCAwLTQuNTYtMi4wNUwuNDE2IDkuNWE2LjI5NyA2LjI5NyAwIDAgMCAyLjA5IDcuMjc4bC4wMTIuMDEuMDMuMDIyIDUuMTYgMy44NjcgNC43NDUtMy41ODQtNy40NDQtNS42MzJaIgogICAgICAgIGZpbGw9IiNGQzZEMjYiLz4KPC9zdmc+Cg=="
       alt="GitLab Logo" />
  <h1>
    %v
  </h1>
  <div class="container">
    <h2>%v</h2>
    <hr />
    %v
    <a href="javascript:history.back()" class="js-go-back go-back">Go back</a>
  </div>
  <script>
    (function () {
      var goBack = document.querySelector('.js-go-back');

      if (history.length > 1) {
        goBack.style.display = 'inline';
      }
    })();
  </script>
</body>
</html>
`

func generateErrorHTML(c content) string {
	return fmt.Sprintf(predefinedErrorPage, c.title, c.statusString, c.header, c.subHeader)
}

func serveErrorPage(w http.ResponseWriter, c content) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(c.status)
	fmt.Fprintln(w, generateErrorHTML(c))
}

// Serve401 returns a 401 error response / HTML page to the http.ResponseWriter
func Serve401(w http.ResponseWriter) {
	serveErrorPage(w, content401)
}

// Serve404 returns a 404 error response / HTML page to the http.ResponseWriter
func Serve404(w http.ResponseWriter) {
	serveErrorPage(w, content404)
}

// Serve414 returns a 414 error response / HTML page to the http.ResponseWriter
func Serve414(w http.ResponseWriter) {
	serveErrorPage(w, content414)
}

// Serve429 returns a 429 error response / HTML page to the http.ResponseWriter
func Serve429(w http.ResponseWriter) {
	serveErrorPage(w, content429)
}

// Serve500 returns a 500 error response / HTML page to the http.ResponseWriter
func Serve500(w http.ResponseWriter) {
	serveErrorPage(w, content500)
}

// Serve500WithRequest returns a 500 error response / HTML page to the http.ResponseWriter
func Serve500WithRequest(w http.ResponseWriter, r *http.Request, reason string, err error) {
	logging.Logger.ErrorContext(logging.LogRequest(r), reason, "error", err)
	errortracking.CaptureErrWithReqAndStackTrace(err, r)
	serveErrorPage(w, content500)
}

// Serve502 returns a 502 error response / HTML page to the http.ResponseWriter
func Serve502(w http.ResponseWriter) {
	serveErrorPage(w, content502)
}

// Serve503 returns a 503 error response / HTML page to the http.ResponseWriter
func Serve503(w http.ResponseWriter) {
	serveErrorPage(w, content503)
}
