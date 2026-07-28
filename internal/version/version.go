// Package version, üç binary'nin ortak sürüm bilgisini taşır.
package version

// Version, derleme sırasında ldflags ile doldurulur:
//
//	go build -ldflags "-X github.com/erkanrzgc/panely/internal/version.Version=v0.1.0"
//
// Doldurulmazsa geliştirme derlemesi olduğu belli olur.
var Version = "dev"

// Commit, derlemenin yapıldığı git commit'i. ldflags ile doldurulur.
var Commit = "unknown"

// Protocol, istemci ↔ panelyd ↔ executor arasındaki sözleşme sürümüdür.
//
// Üç binary aynı anda güncellenmeyebilir: bootstrap sunucuyu günceller ama
// iş istasyonundaki CLI eski kalabilir. Uyumsuz sürümler sessizce yanlış
// davranmak yerine bağlantıyı reddeder.
//
// Bu sayı yalnızca GERİYE UYUMSUZ bir değişiklikte artar. Alan eklemek
// protobuf'ta uyumludur ve sayıyı artırmaz; alan silmek veya anlamını
// değiştirmek artırır.
const Protocol uint32 = 1
