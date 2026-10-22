using System;
using System.Numerics;
using Microsoft.Graphics.Canvas;
using Microsoft.Graphics.Canvas.Geometry;
using Microsoft.Graphics.Canvas.UI;
using Microsoft.Graphics.Canvas.UI.Xaml;
using Microsoft.UI;
using Microsoft.UI.Composition.SystemBackdrops;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Media;
using Windows.UI;

namespace YimengProto;

public sealed partial class MainWindow : Window
{
    private float _drawn;
    private float _t;
    private readonly DispatcherTimer _timer = new() { Interval = TimeSpan.FromMilliseconds(33) };

    public MainWindow()
    {
        InitializeComponent();
        TrySetMica();
        _timer.Tick += (_, _) =>
        {
            _t += 0.033f;
            if (_drawn < 1f) _drawn = MathF.Min(1f, _drawn + 0.033f / 1.6f);
            DoodleCanvas.Invalidate();
            FieldCanvas.Invalidate();
        };
        _timer.Start();
        ExtendsContentIntoTitleBar = true;
    }

    private void TrySetMica()
    {
        if (!MicaController.IsSupported()) return;
        SystemBackdrop = new MicaBackdrop { Kind = MicaKind.Base };
    }

    private void FieldCanvas_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        ds.Clear(Color.FromArgb(255, 0x0A, 0x0A, 0x0C));
        var blobs = new[]
        {
            Color.FromArgb(140, 0x7A, 0x1C, 0xFF),
            Color.FromArgb(140, 0xFF, 0x5F, 0xA8),
            Color.FromArgb(140, 0x33, 0xD1, 0x7A),
            Color.FromArgb(140, 0xFF, 0xD3, 0x6B),
        };
        float w = (float)sender.ActualWidth, h = (float)sender.ActualHeight;
        for (int i = 0; i < blobs.Length; i++)
        {
            float cx = w * (0.25f + 0.2f * i);
            float cy = h * (0.30f + 0.08f * MathF.Sin(_t * 0.35f + i));
            ds.FillCircle(cx, cy, MathF.Min(w, h) * 0.35f, blobs[i]);
        }
    }

    private void DoodleCanvas_CreateResources(CanvasControl sender, CanvasCreateResourcesEventArgs args) { }

    private void DoodleCanvas_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float w = (float)sender.ActualWidth, h = (float)sender.ActualHeight;
        if (w < 1 || h < 1) return;

        float left = w * 0.14f, top = h * 0.22f, side = w * 0.72f;
        using var builder = new CanvasPathBuilder(sender);
        // Outer rounded frame
        builder.BeginFigure(left - 14, top);
        builder.AddLine(left + side + 14, top);
        builder.AddLine(left + side + 14, top + side);
        builder.AddLine(left - 14, top + side);
        builder.EndFigure(CanvasFigureLoop.Closed);
        // Cloud-ish arcs (simplified)
        builder.BeginFigure(left + 10, top - 10);
        builder.AddCubicBezier(new Vector2(left + 10, top - 40), new Vector2(left + 80, top - 50), new Vector2(left + 110, top - 10));
        builder.EndFigure(CanvasFigureLoop.Open);

        using var geo = CanvasGeometry.CreatePath(builder);
        float len = geo.ComputePathLength();
        // Trim via dashed stroke: visible = len*_drawn
        var style = new CanvasStrokeStyle
        {
            DashStyle = CanvasDashStyle.Solid,
            StartCap = CanvasCapStyle.Round,
            EndCap = CanvasCapStyle.Round,
            DashCap = CanvasCapStyle.Round,
            CustomDashStyle = new float[] { MathF.Max(0.01f, len * EaseInOut(_drawn)), len },
        };
        float pulse = 0.025f * 0.75f * MathF.Max(0, MathF.Sin(_t * (96f / 60f) * MathF.PI * 2f));
        float scale = 1f + 0.04f * 0.75f * (pulse * 20f);
        var mid = new Vector2(left + side / 2, top + side / 2);
        ds.Transform = Matrix3x2.CreateScale(scale, mid);
        var white = Color.FromArgb((byte)(255 * (0.6f + 0.3f * 0.75f)), 255, 255, 255);
        ds.DrawGeometry(geo, white, 2.2f, style);
        ds.Transform = Matrix3x2.Identity;
    }

    private static float EaseInOut(float t) =>
        t < 0.5f ? 2 * t * t : 1 - MathF.Pow(-2 * t + 2, 2) / 2;
}
