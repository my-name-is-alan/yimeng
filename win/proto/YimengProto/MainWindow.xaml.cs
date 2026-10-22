using System;
using System.Numerics;
using Microsoft.Graphics.Canvas;
using Microsoft.Graphics.Canvas.Effects;
using Microsoft.Graphics.Canvas.Geometry;
using Microsoft.Graphics.Canvas.UI;
using Microsoft.Graphics.Canvas.UI.Xaml;
using Microsoft.UI;
using Microsoft.UI.Composition;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Hosting;
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
    private readonly DispatcherTimer _timer = new() { Interval = TimeSpan.FromMilliseconds(33) }; // 30fps
    private bool _calm;
    private CanvasRenderTarget? _fieldCache;
    private bool _fieldCached;

    public MainWindow()
    {
        InitializeComponent();
        // No Mica / Acrylic — solid base; glass via Composition blur on panels.
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
                float wall = _t / ( _calm ? 0.08f : 1f);
                float p = Math.Clamp(wall / 1.6f, 0f, 1f);
                _drawn = p * p * (3 - 2 * p); // ease ≈ cubic-bezier
            }
            DoodleCanvas.Invalidate();
            if (!_fieldCached) FieldCanvas.Invalidate();
            else if (!_calm) FieldCanvas.Invalidate(); // breathing only
        };
        _timer.Start();
        ExtendsContentIntoTitleBar = true;
        Activated += (_, _) => AttachGlassBlur();
    }

    private void AttachGlassBlur()
    {
        try
        {
            ApplyBlur(MiniPlayerGlass, 20f, extraTint: true);
            ApplyBlur(TabGlass, 20f, extraTint: false);
            ApplyBlur(OrbGlass, 20f, extraTint: false, orb: true);
        }
        catch { /* Designer machine may lack composition blur; solid tint remains. */ }
    }

    private void ApplyBlur(UIElement el, float blurPx, bool extraTint, bool orb = false)
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
        var factory = compositor.CreateEffectFactory(blur);
        var brush = factory.CreateBrush();
        brush.SetSourceParameter("backdrop", backdrop);
        var sprite = compositor.CreateSpriteVisual();
        sprite.Brush = brush;
        sprite.Size = new Vector2((float)el.ActualSize.X, (float)el.ActualSize.Y);
        el.SizeChanged += (_, __) => sprite.Size = new Vector2((float)el.ActualSize.X, (float)el.ActualSize.Y);
        ElementCompositionPreview.SetElementChildVisual(el, sprite);
        // black 14% tint + optional mini #0A0A0C 55% sit in XAML Background under the sprite
        _ = extraTint; _ = orb;
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
            // 4 static circles (cached) — soft blobs
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0x7A, 0x1A), w * 0.10f, h * 0.08f, w * 0.52f);
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0xD2, 0x3F), w * 0.90f, h * 0.32f, w * 0.48f);
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0x3D, 0x2E), w * 0.05f, h * 0.68f, w * 0.50f);
            DrawSoft(cds, Color.FromArgb(140, 0xFF, 0xB0, 0x00), w * 0.95f, h * 0.92f, w * 0.45f);
            _fieldCached = true;
        }
        ds.DrawImage(_fieldCache);
        // Breathing: fixed 20→420 radial, only opacity, only “real playback” demo
        if (!_calm)
        {
            float pulse = MathF.Exp(-7f * _beatAgo);
            var c = Color.FromArgb((byte)(255 * 0.22f * pulse), 0xFF, 0x7A, 0x1A);
            using var rb = new CanvasRadialGradientBrush(sender, c, Color.FromArgb(0, 0, 0, 0))
            {
                Center = new Vector2(w * 0.5f, h * 0.42f),
                RadiusX = 420, RadiusY = 420,
            };
            ds.FillCircle(w * 0.5f, h * 0.42f, 420, rb);
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

    private void DoodleCanvas_CreateResources(CanvasControl sender, CanvasCreateResourcesEventArgs args) { }

    private void DoodleCanvas_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float w = (float)sender.ActualWidth, h = (float)sender.ActualHeight;
        if (w < 1 || h < 1) return;
        float k = w / 402f;
        float left = 51 * k, top = 150 * k, side = 300 * k;

        using var builder = new CanvasPathBuilder(sender);
        // cloud motif frame + clouds (simplified from DoodlePaths)
        float o = 14;
        builder.BeginFigure(left - o, top);
        builder.AddLine(left + side + o, top);
        builder.AddLine(left + side + o, top + side);
        builder.AddLine(left - o, top + side);
        builder.EndFigure(CanvasFigureLoop.Closed);
        builder.BeginFigure(left - 50, top + 40);
        builder.AddCubicBezier(new Vector2(left - 50 - 8, top + 70), new Vector2(left - 20, top + 50), new Vector2(left - 20, top + 55));
        builder.EndFigure(CanvasFigureLoop.Open);

        using var geo = CanvasGeometry.CreatePath(builder);
        float len = geo.ComputePathLength();
        var style = new CanvasStrokeStyle
        {
            StartCap = CanvasCapStyle.Round, EndCap = CanvasCapStyle.Round,
            CustomDashStyle = new float[] { MathF.Max(0.01f, len * _drawn), len },
        };
        float pulse = _calm ? 0 : 0.04f * Intensity * MathF.Exp(-7f * _beatAgo);
        float scale = 1f + pulse;
        var mid = new Vector2(left + side / 2, top + side / 2);
        ds.Transform = Matrix3x2.CreateScale(scale, mid);
        var glow = Color.FromArgb(128, 255, 255, 255);
        ds.DrawGeometry(geo, glow, 2.23f + 8f, style);
        var white = Color.FromArgb((byte)(255 * (0.6f + 0.3f * Intensity)), 255, 255, 255);
        ds.DrawGeometry(geo, white, 2.23f, style);
        ds.Transform = Matrix3x2.Identity;
    }
}
