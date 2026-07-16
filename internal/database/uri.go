package database

import "net/url"

func readOnlyURI(path string) string {
	return sqliteFileURI(path, url.Values{"mode": {"ro"}})
}

func writableURI(path string) string {
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Set("_txlock", "immediate")
	return sqliteFileURI(path, query)
}

func sqliteFileURI(path string, query url.Values) string {
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}
