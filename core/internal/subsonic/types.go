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
