import Foundation

struct Album: Identifiable, Hashable { let id: String; let title: String; let artist: String; let cover: String; var year = "2023" }
struct Track: Identifiable, Hashable { let id = UUID(); let title: String; let duration: String }
struct Banner: Identifiable { let id: String; let kicker: String; let title: String; let meta: String; var image: String { id } }

#if DEBUG
enum Mock {
    static let banners = [
        Banner(id: "ym_r2", kicker: "每日推荐", title: "深夜电台 · 10月21日", meta: "30 首 · 为你精选"),
        Banner(id: "ym_r1", kicker: "新歌速递", title: "本周华语新歌", meta: "24 首 · 每周五更新"),
        Banner(id: "ym_n2", kicker: "氛围", title: "森林与雨声 · 专注", meta: "42 首 · Lo-fi & Ambient"),
    ]
    static let recent = [
        Album(id: "a1", title: "范特西", artist: "周杰伦", cover: "ym_r1", year: "2001"),
        Album(id: "a2", title: "Random Access Memories", artist: "Daft Punk", cover: "ym_r2", year: "2013"),
        Album(id: "a3", title: "我是一只鱼", artist: "任贤齐", cover: "ym_r3", year: "1997"),
        Album(id: "a4", title: "Blonde", artist: "Frank Ocean", cover: "ym_r4", year: "2016"),
    ]
    static let newIn = [
        Album(id: "n1", title: "山海", artist: "草东没有派对", cover: "ym_n1", year: "2016"),
        Album(id: "n2", title: "In Rainbows", artist: "Radiohead", cover: "ym_n2", year: "2007"),
        Album(id: "n3", title: "无法长大", artist: "赵雷", cover: "ym_n3", year: "2016"),
        Album(id: "n4", title: "Currents", artist: "Tame Impala", cover: "ym_n4", year: "2015"),
    ]
    static let nightfall = Album(id: "nf", title: "夜航星", artist: "不才", cover: "ym_nightfall", year: "2023")
    static let tracks = [Track(title: "夜航星", duration: "4:52"), Track(title: "山河令", duration: "3:41"), Track(title: "一程山路", duration: "5:08"),
                         Track(title: "参商", duration: "4:17"), Track(title: "明月几时", duration: "3:59"), Track(title: "长安忆", duration: "4:33")]
    static let moreTracks = [Track(title: "归途", duration: "4:02"), Track(title: "落笔", duration: "3:28"), Track(title: "星河", duration: "5:11"), Track(title: "北行", duration: "4:44")]
    static let coldplayTop = [("ym_c1", "Yellow", "Parachutes · 2000"), ("ym_c2", "Viva La Vida", "Viva La Vida · 2008"),
                              ("ym_c3", "Fix You", "X&Y · 2005"), ("ym_c4", "The Scientist", "A Rush of Blood · 2002")]
    static let coldplayAlbums = [Album(id: "c5", title: "Moon Music", artist: "2024", cover: "ym_c5"), Album(id: "c2", title: "Viva La Vida", artist: "2008", cover: "ym_c2"),
                                 Album(id: "c3", title: "X&Y", artist: "2005", cover: "ym_c3"), Album(id: "c1", title: "Parachutes", artist: "2000", cover: "ym_c1")]
    static let lyrics: [(TimeInterval, String)] = [(0, "夜色把航线 写进了星图"), (4, "我在云层之上 数着孤独"), (8, "风吹过耳畔 像你在低语"), (12, "梦里的山海 也曾为我停留"),
        (16, "哪怕前路 只剩一盏灯火"), (20, "我也会循着光 一路向北"), (24, "星河滚烫 你是人间理想"), (28, "落笔成诗 难写一句再见"), (32, "夜航的星 不问归途")]
}
#else
/// Release: no demo content ships. Types stay so dead demo views still compile.
enum Mock {
    static let banners: [Banner] = []
    static let recent: [Album] = []
    static let newIn: [Album] = []
    static let nightfall = Album(id: "", title: "", artist: "", cover: "")
    static let tracks: [Track] = []
    static let moreTracks: [Track] = []
    static let coldplayTop: [(String, String, String)] = []
    static let coldplayAlbums: [Album] = []
    static let lyrics: [(TimeInterval, String)] = []
}
#endif
