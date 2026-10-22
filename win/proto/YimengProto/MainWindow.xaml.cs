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
    private readonly float _strokeDp = 1.6f + 0.55f * 1.4f; // 1.6+energy×1.4
    private readonly DispatcherTimer _timer = new() { Interval = TimeSpan.FromMilliseconds(33) };
    private bool _calm;
    private CanvasRenderTarget? _fieldCache;
    private CanvasGeometry? _motif;
    private float _motifW, _motifH;
    private bool _fieldCached;

    public MainWindow()
    {
        InitializeComponent();
        SystemBackdrop = null; // no Mica / Acrylic — opaque RootGrid
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
                _drawn = DoodlePaths.CubicBezierEase(p); // cubic-bezier(0.42,0,0.58,1)
            }
            DoodleCanvas.Invalidate();
            OrbCanvas.Invalidate();
            if (!_fieldCached || !_calm) FieldCanvas.Invalidate();
        };
        _timer.Start();
        // Do NOT ExtendsContentIntoTitleBar with null SystemBackdrop — that punches
        // desktop through the HWND. Keep a normal title bar + fully opaque content.
        ExtendsContentIntoTitleBar = false;
        try
        {
            var opaque = Color.FromArgb(255, 0x0A, 0x0A, 0x0C);
            AppWindow.TitleBar.ButtonBackgroundColor = opaque;
            AppWindow.TitleBar.ButtonInactiveBackgroundColor = opaque;
            AppWindow.TitleBar.BackgroundColor = opaque;
            // Win32: ensure HWND is not layered-transparent
            var hwnd = WinRT.Interop.WindowNative.GetWindowHandle(this);
            ForceOpaque(hwnd);
        }
        catch { }
        Activated += (_, _) => AttachGlassBlur();
        // Default phone-like size so bar+orb+doodle all fit
        try { AppWindow.Resize(new Windows.Graphics.SizeInt32(480, 900)); } catch { }
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
        // Keep layered if needed but force alpha=255 so desktop never bleeds
        int ex = GetWindowLong(hwnd, GWL_EXSTYLE);
        SetWindowLong(hwnd, GWL_EXSTYLE, ex | WS_EX_LAYERED);
        SetLayeredWindowAttributes(hwnd, 0, 255, LWA_ALPHA);
    }

    private void AttachGlassBlur()
    {
        try
        {
            ApplyBlur(MiniPlayerGlass, 20f, extraTint: true);
            ApplyBlur(TabGlass, 20f, extraTint: false);
            ApplyBlur(OrbGlass, 20f, extraTint: false, orb: true);
        }
        catch { }
    }

    /// <summary>
    /// Real blur stack: Backdrop → GaussianBlur 20 → black 14% → (mini) Base #0A0A0C 55%.
    /// XAML also keeps Base 55% / black 14% so panels stay opaque if composition fails.
    /// </summary>
    private void ApplyBlur(FrameworkElement el, float blurPx, bool extraTint, bool orb = false)
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

        // black 14% over blur
        var blackTint = new ColorSourceEffect
        {
            Color = Color.FromArgb(36, 0, 0, 0), // 14%
        };
        IGraphicsEffect graph = new ArithmeticCompositeEffect
        {
            Source1 = blur,
            Source2 = blackTint,
            Source1Amount = 1f,
            Source2Amount = 1f,
            MultiplyAmount = 0f,
            Offset = 0f,
        };

        if (extraTint)
        {
            // Base #0A0A0C 55% — must actually stack (fixes see-through desktop)
            var baseTint = new ColorSourceEffect
            {
                Color = Color.FromArgb(140, 0x0A, 0x0A, 0x0C), // 55%
            };
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
        _ = orb;
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
            cds.Clear(Color.FromArgb(255, 0x0A, 0x0A, 0x0C)); // opaque Base
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0x7A, 0x1A), w * 0.10f, h * 0.08f, w * 0.52f);
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0xD2, 0x3F), w * 0.90f, h * 0.32f, w * 0.48f);
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0x3D, 0x2E), w * 0.05f, h * 0.68f, w * 0.50f);
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0xB0, 0x00), w * 0.95f, h * 0.92f, w * 0.45f);
            _fieldCached = true;
        }
        // BlendMode.Screen, fixed r=420, α=blob0×0.22×pulse
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
        _motif?.Dispose();
        _motif = null;
    }

    private void DoodleCanvas_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float w = (float)sender.ActualWidth, h = (float)sender.ActualHeight;
        if (w < 1 || h < 1) return;

        if (_motif == null || MathF.Abs(_motifW - w) > 0.5f || MathF.Abs(_motifH - h) > 0.5f)
        {
            _motif?.Dispose();
            _motif = DoodlePaths.MotifInk(sender, w, h);
            _motifW = w; _motifH = h;
        }
        float total = _motif.ComputePathLength();
        float target = MathF.Max(0.01f, total * _drawn);

        // Trim ALL contours together via dash over full path length
        var style = new CanvasStrokeStyle
        {
            StartCap = CanvasCapStyle.Round,
            EndCap = CanvasCapStyle.Round,
            DashCap = CanvasCapStyle.Round,
            CustomDashStyle = new float[] { target, total },
        };

        float strokePx = _strokeDp * (96f / 96f); // 1.6+energy×1.4 dp ≈ px at 96dpi; scale with width
        strokePx *= w / 402f; // density-ish
        strokePx = MathF.Max(1.4f, _strokeDp * (w / 402f)); // 1.6+energy×1.4 dp scaled

        float pulse = _calm ? 0 : 0.04f * Intensity * MathF.Exp(-7f * _beatAgo);
        float scale = 1f + pulse;
        float k = w / 402f;
        var mid = new Vector2((128 + 116) * k, (136 + 116) * k);

        // Wire up wobble (amp = jitter×intensity)
        CanvasGeometry drawGeo = _motif;
        CanvasGeometry? wobbleGeo = null;
        float amp = Jitter * Intensity * (_calm ? 0.08f : 1f);
        if (amp > 0.01f)
        {
            wobbleGeo = Wobble(sender, _motif, amp, _t);
            drawGeo = wobbleGeo;
            total = drawGeo.ComputePathLength();
            target = MathF.Max(0.01f, total * _drawn);
            style.CustomDashStyle = new float[] { target, total };
        }

        ds.Transform = Matrix3x2.CreateScale(scale, mid);

        // Glow = white 50% + blur ~4dp (GaussianBlur on stroked command list)
        using (var cl = new CanvasCommandList(sender))
        {
            using (var cds = cl.CreateDrawingSession())
            {
                cds.DrawGeometry(drawGeo, Color.FromArgb(128, 255, 255, 255), strokePx, style);
            }
            float blurPx = 4f * (w / 402f) * 2.5f;
            var glowFx = new GaussianBlurEffect
            {
                Source = cl,
                BlurAmount = MathF.Max(2f, blurPx),
                BorderMode = EffectBorderMode.Soft,
            };
            ds.DrawImage(glowFx);
        }

        // Main stroke alpha 0.6+0.3×intensity
        var white = Color.FromArgb((byte)(255 * (0.6f + 0.3f * Intensity)), 255, 255, 255);
        ds.DrawGeometry(drawGeo, white, strokePx, style);
        ds.Transform = Matrix3x2.Identity;
        wobbleGeo?.Dispose();
    }

    /// <summary>Perpendicular sine offset along flattened path (wobble was computed but unused).</summary>
    private static CanvasGeometry Wobble(ICanvasResourceCreator rc, CanvasGeometry src, float amp, float phase)
    {
        // Approximate: stroke then? Use CreatePath via tessellation points from ComputePointOnPath
        float len = src.ComputePathLength();
        if (len < 1f) return src;
        int steps = Math.Max(2, (int)(len / 3.5f));
        using var b = new CanvasPathBuilder(rc);
        bool started = false;
        for (int i = 0; i <= steps; i++)
        {
            float d = len * i / steps;
            Vector2 pos = src.ComputePointOnPath(d, out Vector2 tan);
            // normalize tan
            float tl = MathF.Sqrt(tan.X * tan.X + tan.Y * tan.Y);
            if (tl > 1e-4f) { tan.X /= tl; tan.Y /= tl; }
            float nx = -tan.Y, ny = tan.X;
            float wob = MathF.Sin(d * 0.085f + phase * 2.4f) * amp;
            var pt = new Vector2(pos.X + nx * wob, pos.Y + ny * wob);
            if (!started) { b.BeginFigure(pt); started = true; }
            else b.AddLine(pt);
        }
        b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    private void OrbCanvas_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float w = (float)sender.ActualWidth, h = (float)sender.ActualHeight;
        if (w < 1 || h < 1) return;
        // Moon + star paths + Crayon double-stroke; no emoji
        float r = 11f * (w / 64f);
        var moonC = new Vector2(w * 0.42f, h * 0.42f);
        using var moon = DoodlePaths.Moon(sender, moonC, r);
        CrayonStroke(ds, moon, Color.FromArgb(230, 255, 255, 255), 2.2f * (w / 64f), w, h);

        using var spark = DoodlePaths.Sparkle(sender, new Vector2(w * 0.72f, h * 0.22f), 4.5f * (w / 64f));
        CrayonStroke(ds, spark, Color.FromArgb(230, 255, 255, 255), 1.6f * (w / 64f), w, h);
    }

    /// <summary>Crayon: main 90% + second pass 35% @ offset(0.8,−0.6) rotate 0.8°.</summary>
    private static void CrayonStroke(CanvasDrawingSession ds, CanvasGeometry geo, Color color, float width, float w, float h)
    {
        var style = new CanvasStrokeStyle { StartCap = CanvasCapStyle.Round, EndCap = CanvasCapStyle.Round };
        var c1 = Color.FromArgb((byte)(color.A * 0.9f), color.R, color.G, color.B);
        ds.DrawGeometry(geo, c1, width, style);
        var prev = ds.Transform;
        ds.Transform = Matrix3x2.CreateTranslation(0.8f, -0.6f)
                     * Matrix3x2.CreateRotation(0.8f * MathF.PI / 180f, new Vector2(w / 2f, h / 2f))
                     * prev;
        var c2 = Color.FromArgb((byte)(color.A * 0.35f), color.R, color.G, color.B);
        ds.DrawGeometry(geo, c2, width * 0.7f, style);
        ds.Transform = prev;
    }

    private void PauseMark_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float w = (float)sender.ActualWidth, h = (float)sender.ActualHeight;
        float barW = w * 0.18f, gap = w * 0.16f, barH = h * 0.55f;
        float top = (h - barH) / 2f, left = w / 2f - gap / 2f - barW;
        ds.FillRoundedRectangle(left, top, barW, barH, barW / 3f, barW / 3f, Colors.White);
        ds.FillRoundedRectangle(left + barW + gap, top, barW, barH, barW / 3f, barW / 3f, Colors.White);
    }

    private void SkipMark_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float w = (float)sender.ActualWidth, h = (float)sender.ActualHeight;
        using var b = new CanvasPathBuilder(sender);
        b.BeginFigure(w * 0.22f, h * 0.22f);
        b.AddLine(w * 0.62f, h * 0.50f);
        b.AddLine(w * 0.22f, h * 0.78f);
        b.EndFigure(CanvasFigureLoop.Closed);
        using var tri = CanvasGeometry.CreatePath(b);
        ds.FillGeometry(tri, Colors.White);
        ds.FillRectangle(w * 0.68f, h * 0.22f, w * 0.12f, h * 0.56f, Colors.White);
    }

    private void DreamRing_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawDreamRing(sender, args, selected: false);

    private void DreamRingSelected_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawDreamRing(sender, args, selected: true);

    private static void DrawDreamRing(CanvasControl sender, CanvasDrawEventArgs args, bool selected)
    {
        var ds = args.DrawingSession;
        float w = (float)sender.ActualWidth;
        float r = w / 2f;
        float a = selected ? 1f : 0.75f;
        var ring = Color.FromArgb((byte)(255 * 0.85f * a), (byte)(selected ? 0xC8 : 0xFF), (byte)(selected ? 0xF5 : 0xFF), (byte)(selected ? 0x42 : 0xFF));
        var sparkC = Color.FromArgb((byte)(255 * 0.95f * a), ring.R, ring.G, ring.B);
        float stroke = MathF.Max(1.2f, 1.4f);
        ds.DrawCircle(r, r, r * 0.72f, ring, stroke);
        using var spark = DoodlePaths.Sparkle(sender, new Vector2(r, r), r * 0.28f);
        ds.DrawGeometry(spark, sparkC, stroke * 0.7f);
    }
}
