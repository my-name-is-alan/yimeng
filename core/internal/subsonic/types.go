package subsonic

type musicFolders struct {
	Folder []musicFolder `xml:"musicFolder" json:"musicFolder"`
}
type musicFolder struct {
	ID   int    `xml:"id,attr" json:"id"`
	Name string `xml:"name,attr" json:"name"`
}

type artistsIdx struct {
	Index []artistIndex `xml:"index" json:"index"`
}
type artistIndex struct {
	Name   string   `xml:"name,attr" json:"name"`
	Artist []artist `xml:"artist" json:"artist"`
}
type indexes struct {
	Index []artistIndex `xml:"index" json:"index"`
}
type artist struct {
	ID         string `xml:"id,attr" json:"id"`
	Name       string `xml:"name,attr" json:"name"`
	AlbumCount int    `xml:"albumCount,attr,omitempty" json:"albumCount,omitempty"`
	CoverArt   string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Starred    string `xml:"starred,attr,omitempty" json:"starred,omitempty"`
}

type artistDetail struct {
	artist
	Album []album `xml:"album" json:"album"`
}

type extension struct {
	Name     string `xml:"name,attr" json:"name"`
	Versions []int  `xml:"versions" json:"versions"`
}

type starred2 struct {
	Artist []artist `xml:"artist" json:"artist"`
	Album  []album  `xml:"album" json:"album"`
	Song   []song   `xml:"song" json:"song"`
}

type lyricsList struct {
	StructuredLyrics []structuredLyrics `xml:"structuredLyrics" json:"structuredLyrics"`
}
type structuredLyrics struct {
	Lang          string      `xml:"lang,attr" json:"lang"`
	Synced        bool        `xml:"synced,attr" json:"synced"`
	DisplayArtist string      `xml:"displayArtist,attr,omitempty" json:"displayArtist,omitempty"`
	DisplayTitle  string      `xml:"displayTitle,attr,omitempty" json:"displayTitle,omitempty"`
	Line          []lyricLine `xml:"line" json:"line"`
}
type lyricLine struct {
	Start *int64 `xml:"start,attr,omitempty" json:"start,omitempty"`
	Value string `xml:",chardata" json:"value"`
}

type user struct {
	Username            string `xml:"username,attr" json:"username"`
	ScrobblingEnabled   bool   `xml:"scrobblingEnabled,attr" json:"scrobblingEnabled"`
	AdminRole           bool   `xml:"adminRole,attr" json:"adminRole"`
	SettingsRole        bool   `xml:"settingsRole,attr" json:"settingsRole"`
	DownloadRole        bool   `xml:"downloadRole,attr" json:"downloadRole"`
	UploadRole          bool   `xml:"uploadRole,attr" json:"uploadRole"`
	PlaylistRole        bool   `xml:"playlistRole,attr" json:"playlistRole"`
	CoverArtRole        bool   `xml:"coverArtRole,attr" json:"coverArtRole"`
	CommentRole         bool   `xml:"commentRole,attr" json:"commentRole"`
	PodcastRole         bool   `xml:"podcastRole,attr" json:"podcastRole"`
	StreamRole          bool   `xml:"streamRole,attr" json:"streamRole"`
	JukeboxRole         bool   `xml:"jukeboxRole,attr" json:"jukeboxRole"`
	ShareRole           bool   `xml:"shareRole,attr" json:"shareRole"`
	VideoConversionRole bool   `xml:"videoConversionRole,attr" json:"videoConversionRole"`
	Folder              []int  `xml:"folder" json:"folder"`
}

type playQueue struct {
	Current   string `xml:"current,attr,omitempty" json:"current,omitempty"`
	Position  int64  `xml:"position,attr" json:"position"`
	Username  string `xml:"username,attr" json:"username"`
	Changed   string `xml:"changed,attr" json:"changed"`
	ChangedBy string `xml:"changedBy,attr" json:"changedBy"`
	Entry     []song `xml:"entry" json:"entry"`
}

type albumList2 struct {
	Album []album `xml:"album" json:"album"`
}
type album struct {
	ID        string `xml:"id,attr" json:"id"`
	Name      string `xml:"name,attr" json:"name"`
	Artist    string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	ArtistID  string `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	CoverArt  string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	SongCount int    `xml:"songCount,attr,omitempty" json:"songCount,omitempty"`
	Duration  int    `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	Year      int    `xml:"year,attr,omitempty" json:"year,omitempty"`
	Genre     string `xml:"genre,attr,omitempty" json:"genre,omitempty"`
	Starred   string `xml:"starred,attr,omitempty" json:"starred,omitempty"`
}

type albumDetail struct {
	album
	Song []song `xml:"song" json:"song"`
}

type song struct {
	ID          string `xml:"id,attr" json:"id"`
	Parent      string `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	Title       string `xml:"title,attr" json:"title"`
	Album       string `xml:"album,attr,omitempty" json:"album,omitempty"`
	Artist      string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	AlbumID     string `xml:"albumId,attr,omitempty" json:"albumId,omitempty"`
	ArtistID    string `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	CoverArt    string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Duration    int    `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	Track       int    `xml:"track,attr,omitempty" json:"track,omitempty"`
	DiscNumber  int    `xml:"discNumber,attr,omitempty" json:"discNumber,omitempty"`
	Year        int    `xml:"year,attr,omitempty" json:"year,omitempty"`
	Genre       string `xml:"genre,attr,omitempty" json:"genre,omitempty"`
	Size        int64  `xml:"size,attr,omitempty" json:"size,omitempty"`
	ContentType string `xml:"contentType,attr,omitempty" json:"contentType,omitempty"`
	Suffix      string `xml:"suffix,attr,omitempty" json:"suffix,omitempty"`
	BitRate     int    `xml:"bitRate,attr,omitempty" json:"bitRate,omitempty"`
	Path        string `xml:"path,attr,omitempty" json:"path,omitempty"`
	Type        string `xml:"type,attr,omitempty" json:"type,omitempty"`
	IsDir       bool   `xml:"isDir,attr" json:"isDir"`
	Starred     string `xml:"starred,attr,omitempty" json:"starred,omitempty"`
}

type searchResult3 struct {
	Artist []artist `xml:"artist" json:"artist"`
	Album  []album  `xml:"album" json:"album"`
	Song   []song   `xml:"song" json:"song"`
}

type playlists struct {
	Playlist []playlist `xml:"playlist" json:"playlist"`
}
type playlist struct {
	ID        string `xml:"id,attr" json:"id"`
	Name      string `xml:"name,attr" json:"name"`
	SongCount int    `xml:"songCount,attr,omitempty" json:"songCount,omitempty"`
	Duration  int    `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	Public    bool   `xml:"public,attr" json:"public"`
	Owner     string `xml:"owner,attr,omitempty" json:"owner,omitempty"`
}
type playlistDetail struct {
	playlist
	Entry []song `xml:"entry" json:"entry"`
}

type lyrics struct {
	Artist string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Title  string `xml:"title,attr,omitempty" json:"title,omitempty"`
	Value  string `xml:",chardata" json:"value"`
}
