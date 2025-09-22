package html

import (
	"careme/internal/config"
	"html/template"
)

// ClarityScript generates the Microsoft Clarity tracking script HTML
func ClarityScript(cfg *config.Config) template.HTML {
	if cfg.Clarity.ProjectID == "" {
		return ""
	}

	script := `<script type="text/javascript">
    (function(c,l,a,r,i,t,y){
        c[a]=c[a]||function(){(c[a].q=c[a].q||[]).push(arguments)};
        t=l.createElement(r);t.async=1;t.src="https://www.clarity.ms/tag/"+i;
        y=l.getElementsByTagName(r)[0];y.parentNode.insertBefore(t,y);
    })(window, document, "clarity", "script", "` + cfg.Clarity.ProjectID + `");
</script>`

	return template.HTML(script)
}
