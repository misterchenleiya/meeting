package buildinfo

var (
	Tag       = "untagged"
	Commit    = "unknown"
	BuildTime = "unknown"
)

type Info struct {
	Tag       string
	Commit    string
	BuildTime string
}

func Current() Info {
	return Info{
		Tag:       Tag,
		Commit:    Commit,
		BuildTime: BuildTime,
	}
}
