package art.yimeng.proto

import androidx.compose.ui.graphics.Color

object YmTheme {
    val base = Color(0xFF0A0A0C) // splash + field base
    val fg = Color.White
    val secondary = Color.White.copy(alpha = 0.60f)
    val accent = Color(0xFFC8F542) // lime
    val fill = Color.White
    val blobs = listOf(
        Color(0xFF7A1CFF), Color(0xFFFF5FA8), Color(0xFF33D17A), Color(0xFFFFD36B),
    )
    val idleField = base
}
