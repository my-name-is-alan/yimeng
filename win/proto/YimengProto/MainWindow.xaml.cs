using System;
using System.Numerics;
using Microsoft.Graphics.Canvas;
using Microsoft.Graphics.Canvas.Brushes;
using Microsoft.Graphics.Canvas.Effects;
using Microsoft.Graphics.Canvas.Geometry;
using Microsoft.Graphics.Canvas.UI;
using Microsoft.Graphics.Canvas.UI.Xaml;
using Microsoft.UI;
using Microsoft.UI.Composition;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Hosting;
using Microsoft.UI.Xaml.Input;
using Windows.Graphics.Effects;
using Windows.UI;
using Windows.UI.ViewManagement;

namespace YimengProto;

public sealed partial class MainWindow : Window
{
    private float _drawn;
    private float _t;
    private float _beatAgo = 0.15f;
    private const float Bpm = 96f;
    private const float Intensity = 0.7f;
    private const float Energy = 0.55f;
    private const float Jitter = 2.875f; // 1.5 + energy×2.5
    private readonly float _strokeDp = 1.6f + 0.55f * 1.4f;
    private readonly DispatcherTimer _timer = new() { Interval = TimeSpan.FromMilliseconds(33) };
    private bool _calm;
    private CanvasRenderTarget? _fieldCache;
    private CanvasGeometry[]? _motifParts;
    private float _motifW, _motifH;
    private bool _fieldCached;
    // Zig demo energy for star density; orange/ink motif uses Energy; stars only when _useZigStars
    private const bool UseZigStars = true; // show Int(4+energy×8) stars for r3 visual check
    private const ulong TrackSeed = 42;

    public MainWindow()
    {
        InitializeComponent();
        SystemBackdrop = null;
        _calm = !new UISettings().AnimationsEnabled;
        _timer.Interval = TimeSpan.FromMilliseconds(_calm ? 250 : 33);
        _timer.Tick += (_, _) =>
        {
            float dt = (float)_timer.Interval.TotalSeconds * (_calm ? 0.08f : 1f);
            _t += dt;
            _beatAgo += (float)_timer.Interval.TotalSeconds;
            float period = 60f / Bpm;
            if (_beatAgo >= period) _beatAgo -= period;
            if (_drawn < 1f)
            {
                float wall = _t / (_calm ? 0.08f : 1f);
                float p = Math.Clamp(wall / 1.6f, 0f, 1f);
                _drawn = DoodlePaths.CubicBezierEase(p);
            }
            DoodleCanvas.Invalidate();
            if (!_fieldCached || !_calm) FieldCanvas.Invalidate();
        };
        _timer.Start();
        ExtendsContentIntoTitleBar = false;
        try
        {
            var opaque = Color.FromArgb(255, 0x0A, 0x0A, 0x0C);
            AppWindow.TitleBar.ButtonBackgroundColor = opaque;
            AppWindow.TitleBar.ButtonInactiveBackgroundColor = opaque;
            AppWindow.TitleBar.BackgroundColor = opaque;
            var hwnd = WinRT.Interop.WindowNative.GetWindowHandle(this);
            ForceOpaque(hwnd);
        }
        catch { }
        Activated += (_, _) => AttachGlassBlur();
        // Desktop landscape — NavigationSplitView shell
        try { AppWindow.Resize(new Windows.Graphics.SizeInt32(1280, 800)); } catch { }
    }

    [System.Runtime.InteropServices.DllImport("user32.dll")]
    private static extern int SetWindowLong(IntPtr hWnd, int nIndex, int dwNewLong);
    [System.Runtime.InteropServices.DllImport("user32.dll")]
    private static extern int GetWindowLong(IntPtr hWnd, int nIndex);
    [System.Runtime.InteropServices.DllImport("user32.dll")]
    private static extern bool SetLayeredWindowAttributes(IntPtr hwnd, uint crKey, byte bAlpha, uint dwFlags);
    private const int GWL_EXSTYLE = -20;
    private const int WS_EX_LAYERED = 0x80000;
    private const uint LWA_ALPHA = 0x2;

    private static void ForceOpaque(IntPtr hwnd)
    {
        int ex = GetWindowLong(hwnd, GWL_EXSTYLE);
        SetWindowLong(hwnd, GWL_EXSTYLE, ex | WS_EX_LAYERED);
        SetLayeredWindowAttributes(hwnd, 0, 255, LWA_ALPHA);
    }

    private void AttachGlassBlur()
    {
        try { ApplyBlur(MiniPlayerGlass, 20f, miniBase: true); }
        catch { }
    }

    /// <summary>
    /// Liquid glass: Backdrop → GaussianBlur → sprite Opacity=0.35 → black 14%.
    /// Mini: + Base #0A0A0C 55% as a SINGLE layer (XAML Background is Transparent).
    /// </summary>
    private void ApplyBlur(FrameworkElement el, float blurPx, bool miniBase)
    {
        var visual = ElementCompositionPreview.GetElementVisual(el);
        var compositor = visual.Compositor;
        var backdrop = compositor.CreateBackdropBrush();

        var blur = new GaussianBlurEffect
        {
            Name = "Blur",
            BlurAmount = blurPx,
            BorderMode = EffectBorderMode.Hard,
            Source = new CompositionEffectSourceParameter("backdrop"),
        };
        // Haze α=0.35 on blur only (not on tint layers)
        var haze = new OpacityEffect { Source = blur, Opacity = 0.35f };

        // black 14% over haze
        var blackTint = new ColorSourceEffect { Color = Color.FromArgb(36, 0, 0, 0) };
        IGraphicsEffect graph = new ArithmeticCompositeEffect
        {
            Source1 = haze,
            Source2 = blackTint,
            Source1Amount = 1f,
            Source2Amount = 1f,
            MultiplyAmount = 0f,
            Offset = 0f,
        };

        if (miniBase)
        {
            // Base #0A0A0C 55% — single layer (XAML Background is Transparent)
            var baseTint = new ColorSourceEffect { Color = Color.FromArgb(140, 0x0A, 0x0A, 0x0C) };
            graph = new ArithmeticCompositeEffect
            {
                Source1 = graph,
                Source2 = baseTint,
                Source1Amount = 1f,
                Source2Amount = 1f,
                MultiplyAmount = 0f,
                Offset = 0f,
            };
        }

        var factory = compositor.CreateEffectFactory(graph);
        var brush = factory.CreateBrush();
        brush.SetSourceParameter("backdrop", backdrop);
        var sprite = compositor.CreateSpriteVisual();
        sprite.Brush = brush;
        sprite.RelativeSizeAdjustment = Vector2.One;
        ElementCompositionPreview.SetElementChildVisual(el, sprite);
    }

    private void FieldCanvas_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float w = (float)sender.ActualWidth, h = (float)sender.ActualHeight;
        if (w < 1 || h < 1) return;

        if (_fieldCache == null || _fieldCache.Size.Width != w || _fieldCache.Size.Height != h)
        {
            _fieldCache?.Dispose();
            _fieldCache = new CanvasRenderTarget(sender, w, h, 96);
            using var cds = _fieldCache.CreateDrawingSession();
            cds.Clear(Color.FromArgb(255, 0x0A, 0x0A, 0x0C));
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0x7A, 0x1A), w * 0.30f, h * 0.22f, w * 0.55f);
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0xD2, 0x3F), w * 0.78f, h * 0.38f, w * 0.48f);
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0x3D, 0x2E), w * 0.18f, h * 0.72f, w * 0.50f);
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0xB0, 0x00), w * 0.85f, h * 0.88f, w * 0.42f);
            _fieldCached = true;
        }
        if (!_calm && _fieldCache != null)
        {
            float pulse = MathF.Exp(-7f * _beatAgo);
            float blob0A = 140f / 255f;
            byte a = (byte)Math.Clamp(255 * blob0A * 0.22f * pulse, 0, 255);
            var c = Color.FromArgb(a, 0xFF, 0x7A, 0x1A);
            using var cl = new CanvasCommandList(sender);
            using (var cds = cl.CreateDrawingSession())
            {
                using var rb = new CanvasRadialGradientBrush(sender, c, Color.FromArgb(0, 0, 0, 0))
                {
                    Center = new Vector2(w * 0.5f, h * 0.42f),
                    RadiusX = 420, RadiusY = 420,
                };
                cds.FillCircle(w * 0.5f, h * 0.42f, 420, rb);
            }
            var screen = new BlendEffect
            {
                Mode = BlendEffectMode.Screen,
                Background = _fieldCache,
                Foreground = cl,
            };
            ds.DrawImage(screen);
        }
        else
        {
            ds.DrawImage(_fieldCache);
        }
    }

    private static void DrawSoft(CanvasDrawingSession ds, Color c, float x, float y, float r)
    {
        using var brush = new CanvasRadialGradientBrush(ds, c, Color.FromArgb(0, c.R, c.G, c.B))
        {
            Center = new Vector2(x, y), RadiusX = r, RadiusY = r,
        };
        ds.FillCircle(x, y, r, brush);
    }

    private void DoodleCanvas_CreateResources(CanvasControl sender, CanvasCreateResourcesEventArgs args)
    {
        DisposeMotif();
    }

    private void DisposeMotif()
    {
        if (_motifParts == null) return;
        foreach (var g in _motifParts) g.Dispose();
        _motifParts = null;
    }

    private void DoodleCanvas_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float w = (float)sender.ActualWidth, h = (float)sender.ActualHeight;
        if (w < 1 || h < 1) return;

        // Cover slot centered (iOS player page drip layout scaled into host)
        float cover = MathF.Min(w, h) * 0.72f;
        float left = (w - cover) / 2f;
        float top = (h - cover) / 2f;

        if (_motifParts == null || MathF.Abs(_motifW - w) > 0.5f || MathF.Abs(_motifH - h) > 0.5f)
        {
            DisposeMotif();
            _motifParts = DoodlePaths.MotifInkParts(sender, left, top, cover);
            _motifW = w; _motifH = h;
        }

        float strokePx = MathF.Max(1.4f, _strokeDp * (cover / 232f));
        float pulse = _calm ? 0 : 0.04f * Intensity * MathF.Exp(-7f * _beatAgo);
        float scale = 1f + pulse;
        var mid = new Vector2(left + cover / 2f, top + cover / 2f);

        float amp = Jitter * Intensity * (_calm ? 0.08f : 1f);

        // Cumulative trim across ALL contours (parts)
        float totalLen = 0f;
        foreach (var p in _motifParts) totalLen += p.ComputePathLength();
        float target = MathF.Max(0.01f, totalLen * _drawn);

        ds.Transform = Matrix3x2.CreateScale(scale, mid);

        float acc = 0f;
        var white = Color.FromArgb((byte)(255 * (0.6f + 0.3f * Intensity)), 255, 255, 255);
        var style = new CanvasStrokeStyle
        {
            StartCap = CanvasCapStyle.Round,
            EndCap = CanvasCapStyle.Round,
            DashCap = CanvasCapStyle.Round,
        };

        using (var cl = new CanvasCommandList(sender))
        {
            using (var cds = cl.CreateDrawingSession())
            {
                DrawParts(cds, sender, _motifParts, amp, target, ref acc, Color.FromArgb(128, 255, 255, 255), strokePx, style);
            }
            float blurPx = MathF.Max(2f, 4f * (cover / 232f) * 2.5f);
            var glowFx = new GaussianBlurEffect
            {
                Source = cl,
                BlurAmount = blurPx,
                BorderMode = EffectBorderMode.Soft,
            };
            ds.DrawImage(glowFx);
        }

        acc = 0f;
        DrawParts(ds, sender, _motifParts, amp, target, ref acc, white, strokePx, style);

        // Zig stars: Int(4+energy×8), seed+99, r 4–10, α 0.5–0.95
        if (UseZigStars && _drawn > 0.05f)
        {
            int density = (int)(4 + Energy * 8); // orange energy 0.55 → 8; zig 0.85 → 10
            // Use zig energy 0.85 for star count demo when UseZigStars
            density = (int)(4 + 0.85 * 8);
            ulong state = TrackSeed + 99;
            float strokeStar = strokePx * 0.8f;
            for (int i = 0; i < density; i++)
            {
                float sx = (float)DoodlePaths.SeededRNG.D(ref state, 20, w - 20);
                float sy = (float)DoodlePaths.SeededRNG.D(ref state, top - 50, top + cover + 60);
                float sr = (float)DoodlePaths.SeededRNG.D(ref state, 4, 10);
                float sa = (float)DoodlePaths.SeededRNG.D(ref state, 0.5, 0.95);
                float rot = (float)DoodlePaths.SeededRNG.D(ref state, 0, 1);
                using var star = DoodlePaths.Star(sender, new Vector2(sx, sy), sr, rot);
                byte a = (byte)(255 * sa * _drawn);
                ds.DrawGeometry(star, Color.FromArgb(a, 255, 255, 255), strokeStar, style);
            }
        }

        ds.Transform = Matrix3x2.Identity;
    }

    /// <summary>Wobble + dash-trim EACH contour separately (no V-connector across figures).</summary>
    private void DrawParts(
        CanvasDrawingSession ds, ICanvasResourceCreator rc, CanvasGeometry[] parts,
        float amp, float target, ref float acc, Color ink, float strokePx, CanvasStrokeStyle style)
    {
        foreach (var part in parts)
        {
            float len = part.ComputePathLength();
            CanvasGeometry? wob = null;
            CanvasGeometry draw = part;
            if (amp > 0.01f)
            {
                wob = WobbleOne(rc, part, amp, _t);
                draw = wob;
                len = draw.ComputePathLength();
            }

            float remain = target - acc;
            if (remain > 0.5f)
            {
                float dash = MathF.Min(remain, len);
                style.CustomDashStyle = new float[] { dash, len + 1f };
                ds.DrawGeometry(draw, ink, strokePx, style);
            }
            acc += len;
            wob?.Dispose();
        }
        style.CustomDashStyle = null;
    }

    /// <summary>Perpendicular sine offset along a SINGLE contour (open path).</summary>
    private static CanvasGeometry WobbleOne(ICanvasResourceCreator rc, CanvasGeometry src, float amp, float phase)
    {
        float len = src.ComputePathLength();
        if (len < 1f) return src;
        int steps = Math.Max(2, (int)(len / 3.5f));
        using var b = new CanvasPathBuilder(rc);
        for (int i = 0; i <= steps; i++)
        {
            float d = len * i / steps;
            Vector2 pos = src.ComputePointOnPath(d, out Vector2 tan);
            float tl = MathF.Sqrt(tan.X * tan.X + tan.Y * tan.Y);
            if (tl > 1e-4f) { tan.X /= tl; tan.Y /= tl; }
            float nx = -tan.Y, ny = tan.X;
            float wob = MathF.Sin(d * 0.085f + phase * 2.4f) * amp;
            var pt = new Vector2(pos.X + nx * wob, pos.Y + ny * wob);
            if (i == 0) b.BeginFigure(pt);
            else b.AddLine(pt);
        }
        b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    private void SideHome_Click(object sender, PointerRoutedEventArgs e) { }

    private void GlyphPause_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Pause, filled: true, ink: Colors.White);

    private void GlyphPauseBig_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Pause, filled: true, ink: Colors.White);

    private void GlyphNext_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Next, filled: true, ink: Colors.White);

    private void GlyphPrev_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Previous, filled: true, ink: Colors.White);

    private void GlyphHomeActive_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Home, ink: Color.FromArgb(255, 0xC8, 0xF5, 0x42));

    private void GlyphLibrary_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Library, ink: Color.FromArgb(191, 255, 255, 255));

    private void GlyphCollection_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Collection, ink: Color.FromArgb(191, 255, 255, 255));

    private void GlyphSearch_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Search, ink: Color.FromArgb(191, 255, 255, 255));

    private void GlyphMe_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Me, ink: Color.FromArgb(191, 255, 255, 255));

    private void BrandMark_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float s = (float)Math.Min(sender.ActualWidth, sender.ActualHeight);
        if (s < 1) return;
        YMIconPaths.DrawBrandMark(ds, sender, s, accentSparkle: true);
    }

    private void IconStrip_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float h = (float)sender.ActualHeight;
        if (h < 1) return;
        var glyphs = new[]
        {
            YMGlyph.Home, YMGlyph.Library, YMGlyph.Collection, YMGlyph.Search, YMGlyph.Me,
            YMGlyph.Play, YMGlyph.Pause, YMGlyph.Previous, YMGlyph.Next,
            YMGlyph.Heart, YMGlyph.Star, YMGlyph.Recognize,
        };
        float cell = h;
        float gap = 6f;
        float x = 0;
        var ink = Color.FromArgb(180, 255, 255, 255);
        foreach (var g in glyphs)
        {
            ds.Transform = Matrix3x2.CreateTranslation(x, 0);
            YMIconPaths.DrawIcon(ds, sender, g, cell * 0.9f, ink, weight: 1.5f);
            ds.Transform = Matrix3x2.Identity;
            x += cell + gap;
        }
    }

    private static void DrawGlyph(CanvasControl sender, CanvasDrawEventArgs args, YMGlyph g, bool filled = false, Color? ink = null)
    {
        var ds = args.DrawingSession;
        float s = (float)Math.Min(sender.ActualWidth, sender.ActualHeight);
        if (s < 1) return;
        YMIconPaths.DrawIcon(ds, sender, g, s, ink ?? Colors.White, weight: 1.7f, filled: filled);
    }
}
