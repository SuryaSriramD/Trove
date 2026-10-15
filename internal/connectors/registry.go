package connectors

func NewAdapter(kind Kind) (Adapter, error) {
	switch kind {
	case GitHub:
		return NewGitHubAdapter(), nil
	case GoogleCalendar, GoogleDrive, Gmail:
		return NewGoogleAdapter(kind), nil
	default:
		return nil, Unsupported(string(kind))
	}
}
