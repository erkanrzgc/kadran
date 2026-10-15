package domaincheck

// Record, panelde eklenecek ya da silinecek tek DNS kaydı (K-151).
type Record struct {
	// Type: A, AAAA ya da CNAME.
	Type string
	// Name: bölge içindeki ad; bölgenin kendisi "@".
	Name  string
	Value string
}
