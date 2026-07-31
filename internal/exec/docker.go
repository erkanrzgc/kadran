package exec

// DefaultDockerSocket, Docker Engine API'sinin varsayılan unix soketidir.
//
// # Neden ayrı dosyada?
//
// Yoklama kodunun kendisi Linux'a özgü (dockerprobe.go, //go:build linux),
// ama bu sabit yapılandırma varsayılanı olarak her platformda gerekiyor:
// executor'ın sunucu kurulumu onu okuyor ve o kod platformdan bağımsız.
//
// Sabit de linux dosyasındayken Windows derlemesi kırılmıştı — derleyici
// yakaladı, ama ayrımın nerede geçtiğini burada yazılı tutmak aynı hatanın
// tekrarını engelliyor: PLATFORMA ÖZGÜ OLAN DAVRANIŞ, yapılandırma değil.
const DefaultDockerSocket = "/run/docker.sock"
