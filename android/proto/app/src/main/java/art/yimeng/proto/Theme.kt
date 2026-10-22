package art.yimeng.proto

import androidx.compose.ui.graphics.Color

/** Tokens from docs/cross-platform/design-spec.md §1 (dark mode). */
object YmTheme {
    val accent = Color(0xFFC8F542)
    val base = Color(0xFF0A0A0C)
    val fg = Color.White
    val secondary = Color.White.copy(alpha = 0.60f)
    val fill = Color.White.copy(alpha = 0.08f)
    val divider = Color.White.copy(alpha = 0.06f)

    /** Default dream blobs when no cover colors. */
    val blobs = listOf(
        Color(0xFF7A1CFF),
        Color(0xFFFF5FA8),
        Color(0xFF33D17A),
        Color(0xFFFFD36B),
    )
    val idleField = Color(0xFF1A1028)
    val margin = 20
}
