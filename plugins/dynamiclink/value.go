package dynamiclink

type Value struct {
	Category          string         `json:"category,omitempty"`
	URL               string         `json:"url"`
	Mode              string         `json:"mode"`
	SaveCooke         bool           `json:"saveCooke"`
	ChangeClient      bool           `json:"changeClient"`
	ShowLoader        bool           `json:"showLoader"`
	OpenURLsInBrowser bool           `json:"openUrlsInBrowser"`
	SkipWarningDialog bool           `json:"skipWarningDialog"`
	WarningDialog     *WarningDialog `json:"warningDialog,omitempty"`
	Title             string         `json:"title,omitempty"`
	TrackName         string         `json:"trackName,omitempty"`
}
type WarningDialog struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}
