using System;
using System.Numerics;
using Microsoft.Graphics.Canvas;
using Microsoft.Graphics.Canvas.Geometry;
using Windows.UI;
// Colors via Windows.UI.Color.FromArgb

namespace YimengProto;

/// <summary>Port of iOS YMIcons.swift @ 06f2059 — unit-square 0…1 path tables. No invented glyphs.</summary>
public enum YMGlyph
{
    Home, Library, Collection, Search, Me,
    Play, Pause, Previous, Next, Shuffle, Repeat, RepeatOne, Order, Lyrics, Queue, More,
    Heart, HeartFill, Star, Download, Share, Settings, Back, Close, Recognize, Chevron,
}

public static class YMIconPaths
{
    public static CanvasGeometry Path(ICanvasResourceCreator rc, YMGlyph g) => g switch
    {
        YMGlyph.Home => House(rc),
        YMGlyph.Library => Notes(rc),
        YMGlyph.Collection => Stacks(rc),
        YMGlyph.Search => Magnifier(rc),
        YMGlyph.Me => Person(rc),
        YMGlyph.Play => Play(rc),
        YMGlyph.Pause => Pause(rc),
        YMGlyph.Previous => Skip(rc, back: true),
        YMGlyph.Next => Skip(rc, back: false),
        YMGlyph.Shuffle => Shuffle(rc),
        YMGlyph.Repeat => Loop(rc, one: false),
        YMGlyph.RepeatOne => Loop(rc, one: true),
        YMGlyph.Order => Arrow(rc),
        YMGlyph.Lyrics => Quote(rc),
        YMGlyph.Queue => List(rc),
        YMGlyph.More => Dots(rc),
        YMGlyph.Heart => Heart(rc),
        YMGlyph.HeartFill => Heart(rc),
        YMGlyph.Star => Star(rc),
        YMGlyph.Download => Download(rc),
        YMGlyph.Share => Share(rc),
        YMGlyph.Settings => Gear(rc),
        YMGlyph.Back => Chevron(rc, left: true),
        YMGlyph.Close => Xmark(rc),
        YMGlyph.Recognize => Wave(rc),
        YMGlyph.Chevron => Chevron(rc, left: false),
        _ => House(rc),
    };

    public static CanvasGeometry Scaled(ICanvasResourceCreator rc, YMGlyph g, float sizePx)
    {
        using var unit = Path(rc, g);
        return unit.Transform(Matrix3x2.CreateScale(sizePx));
    }

    static CanvasGeometry House(ICanvasResourceCreator rc)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.50f, 0.14f);
        b.AddLine(0.88f, 0.46f); b.AddLine(0.78f, 0.46f); b.AddLine(0.78f, 0.86f);
        b.AddLine(0.55f, 0.86f); b.AddLine(0.55f, 0.62f); b.AddLine(0.45f, 0.62f);
        b.AddLine(0.45f, 0.86f); b.AddLine(0.22f, 0.86f); b.AddLine(0.22f, 0.46f);
        b.AddLine(0.12f, 0.46f);
        b.EndFigure(CanvasFigureLoop.Closed);
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Notes(ICanvasResourceCreator rc)
    {
        using var e1 = CanvasGeometry.CreateEllipse(rc, 0.29f, 0.71f, 0.11f, 0.09f);
        using var e2 = CanvasGeometry.CreateEllipse(rc, 0.66f, 0.64f, 0.11f, 0.09f);
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.38f, 0.70f); b.AddLine(0.38f, 0.22f); b.AddLine(0.75f, 0.16f); b.AddLine(0.75f, 0.63f);
        b.EndFigure(CanvasFigureLoop.Open);
        using var stem = CanvasGeometry.CreatePath(b);
        return CanvasGeometry.CreateGroup(rc, new[] { e1, e2, stem });
    }

    static CanvasGeometry Stacks(ICanvasResourceCreator rc)
    {
        var parts = new CanvasGeometry[3];
        for (int i = 0; i < 3; i++)
        {
            float o = i * 0.08f;
            parts[i] = CanvasGeometry.CreateRoundedRectangle(rc, 0.18f + o, 0.28f - o * 0.5f, 0.58f, 0.42f, 0.08f, 0.08f);
        }
        var g = CanvasGeometry.CreateGroup(rc, parts);
        foreach (var p in parts) p.Dispose();
        return g;
    }

    static CanvasGeometry Magnifier(ICanvasResourceCreator rc)
    {
        using var ring = CanvasGeometry.CreateEllipse(rc, 0.44f, 0.40f, 0.26f, 0.26f);
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.62f, 0.58f); b.AddLine(0.86f, 0.84f); b.EndFigure(CanvasFigureLoop.Open);
        using var handle = CanvasGeometry.CreatePath(b);
        return CanvasGeometry.CreateGroup(rc, new[] { ring, handle });
    }

    static CanvasGeometry Person(ICanvasResourceCreator rc)
    {
        using var head = CanvasGeometry.CreateEllipse(rc, 0.50f, 0.30f, 0.18f, 0.18f);
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.16f, 0.88f);
        b.AddQuadraticBezier(new Vector2(0.50f, 0.48f), new Vector2(0.84f, 0.88f));
        b.EndFigure(CanvasFigureLoop.Open);
        using var body = CanvasGeometry.CreatePath(b);
        return CanvasGeometry.CreateGroup(rc, new[] { head, body });
    }

    static CanvasGeometry Play(ICanvasResourceCreator rc)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.32f, 0.18f); b.AddLine(0.82f, 0.50f); b.AddLine(0.32f, 0.82f);
        b.EndFigure(CanvasFigureLoop.Closed);
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Pause(ICanvasResourceCreator rc)
    {
        using var a = CanvasGeometry.CreateRoundedRectangle(rc, 0.28f, 0.18f, 0.16f, 0.64f, 0.04f, 0.04f);
        using var c = CanvasGeometry.CreateRoundedRectangle(rc, 0.56f, 0.18f, 0.16f, 0.64f, 0.04f, 0.04f);
        return CanvasGeometry.CreateGroup(rc, new[] { a, c });
    }

    // Previous = |◀ (back=true); Next = ▶| (back=false).
    static CanvasGeometry Skip(ICanvasResourceCreator rc, bool back)
    {
        using var b = new CanvasPathBuilder(rc);
        if (back)
        {
            b.BeginFigure(0.18f, 0.20f); b.AddLine(0.18f, 0.80f); b.EndFigure(CanvasFigureLoop.Open);
            b.BeginFigure(0.82f, 0.18f); b.AddLine(0.38f, 0.50f); b.AddLine(0.82f, 0.82f);
            b.EndFigure(CanvasFigureLoop.Closed);
        }
        else
        {
            b.BeginFigure(0.18f, 0.18f); b.AddLine(0.62f, 0.50f); b.AddLine(0.18f, 0.82f);
            b.EndFigure(CanvasFigureLoop.Closed);
            b.BeginFigure(0.82f, 0.20f); b.AddLine(0.82f, 0.80f); b.EndFigure(CanvasFigureLoop.Open);
        }
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Shuffle(ICanvasResourceCreator rc)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.12f, 0.30f); b.AddLine(0.32f, 0.30f);
        b.AddQuadraticBezier(new Vector2(0.50f, 0.30f), new Vector2(0.68f, 0.70f)); b.AddLine(0.88f, 0.70f);
        b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.78f, 0.58f); b.AddLine(0.90f, 0.70f); b.AddLine(0.78f, 0.82f); b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.12f, 0.70f); b.AddLine(0.32f, 0.70f);
        b.AddQuadraticBezier(new Vector2(0.50f, 0.70f), new Vector2(0.68f, 0.30f)); b.AddLine(0.88f, 0.30f);
        b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.78f, 0.18f); b.AddLine(0.90f, 0.30f); b.AddLine(0.78f, 0.42f); b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Loop(ICanvasResourceCreator rc, bool one)
    {
        using var b = new CanvasPathBuilder(rc);
        float start = -40f * MathF.PI / 180f, end = 220f * MathF.PI / 180f;
        for (int i = 0; i <= 24; i++)
        {
            float t = i / 24f; float a = start + (end - start) * t;
            var pt = new Vector2(0.50f + MathF.Cos(a) * 0.32f, 0.50f + MathF.Sin(a) * 0.32f);
            if (i == 0) b.BeginFigure(pt); else b.AddLine(pt);
        }
        b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.72f, 0.18f); b.AddLine(0.86f, 0.32f); b.AddLine(0.68f, 0.36f); b.EndFigure(CanvasFigureLoop.Open);
        if (one)
        {
            b.BeginFigure(0.46f, 0.38f); b.AddLine(0.52f, 0.38f); b.AddLine(0.52f, 0.68f); b.EndFigure(CanvasFigureLoop.Open);
        }
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Arrow(ICanvasResourceCreator rc)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.18f, 0.50f); b.AddLine(0.78f, 0.50f); b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.62f, 0.32f); b.AddLine(0.82f, 0.50f); b.AddLine(0.62f, 0.68f); b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Quote(ICanvasResourceCreator rc)
    {
        using var e1 = CanvasGeometry.CreateEllipse(rc, 0.29f, 0.33f, 0.11f, 0.11f);
        using var e2 = CanvasGeometry.CreateEllipse(rc, 0.66f, 0.33f, 0.11f, 0.11f);
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.28f, 0.44f); b.AddLine(0.28f, 0.72f); b.AddLine(0.42f, 0.72f); b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.65f, 0.44f); b.AddLine(0.65f, 0.72f); b.AddLine(0.79f, 0.72f); b.EndFigure(CanvasFigureLoop.Open);
        using var stems = CanvasGeometry.CreatePath(b);
        return CanvasGeometry.CreateGroup(rc, new[] { e1, e2, stems });
    }

    static CanvasGeometry List(ICanvasResourceCreator rc)
    {
        using var b = new CanvasPathBuilder(rc);
        foreach (float y in new[] { 0.28f, 0.50f, 0.72f })
        {
            b.BeginFigure(0.36f, y); b.AddLine(0.84f, y); b.EndFigure(CanvasFigureLoop.Open);
        }
        using var lines = CanvasGeometry.CreatePath(b);
        var dots = new CanvasGeometry[]
        {
            CanvasGeometry.CreateEllipse(rc, 0.21f, 0.28f, 0.05f, 0.05f),
            CanvasGeometry.CreateEllipse(rc, 0.21f, 0.50f, 0.05f, 0.05f),
            CanvasGeometry.CreateEllipse(rc, 0.21f, 0.72f, 0.05f, 0.05f),
            lines,
        };
        var g = CanvasGeometry.CreateGroup(rc, dots);
        dots[0].Dispose(); dots[1].Dispose(); dots[2].Dispose();
        return g;
    }

    static CanvasGeometry Dots(ICanvasResourceCreator rc)
    {
        var parts = new[]
        {
            CanvasGeometry.CreateEllipse(rc, 0.29f, 0.49f, 0.07f, 0.07f),
            CanvasGeometry.CreateEllipse(rc, 0.52f, 0.49f, 0.07f, 0.07f),
            CanvasGeometry.CreateEllipse(rc, 0.75f, 0.49f, 0.07f, 0.07f),
        };
        var g = CanvasGeometry.CreateGroup(rc, parts);
        foreach (var p in parts) p.Dispose();
        return g;
    }

    static CanvasGeometry Heart(ICanvasResourceCreator rc)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.50f, 0.82f);
        b.AddCubicBezier(new Vector2(0.20f, 0.68f), new Vector2(0.08f, 0.52f), new Vector2(0.14f, 0.38f));
        b.AddCubicBezier(new Vector2(0.18f, 0.18f), new Vector2(0.38f, 0.16f), new Vector2(0.50f, 0.28f));
        b.AddCubicBezier(new Vector2(0.62f, 0.16f), new Vector2(0.82f, 0.18f), new Vector2(0.86f, 0.38f));
        b.AddCubicBezier(new Vector2(0.92f, 0.52f), new Vector2(0.80f, 0.68f), new Vector2(0.50f, 0.82f));
        b.EndFigure(CanvasFigureLoop.Closed);
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Star(ICanvasResourceCreator rc) =>
        DoodlePaths.Sparkle(rc, new Vector2(0.5f, 0.5f), 0.36f);

    static CanvasGeometry Download(ICanvasResourceCreator rc)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.50f, 0.14f); b.AddLine(0.50f, 0.62f); b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.32f, 0.46f); b.AddLine(0.50f, 0.66f); b.AddLine(0.68f, 0.46f); b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.22f, 0.78f); b.AddLine(0.78f, 0.78f); b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Share(ICanvasResourceCreator rc)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.50f, 0.70f); b.AddLine(0.50f, 0.18f); b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.32f, 0.34f); b.AddLine(0.50f, 0.14f); b.AddLine(0.68f, 0.34f); b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.22f, 0.48f); b.AddLine(0.22f, 0.84f); b.AddLine(0.78f, 0.84f); b.AddLine(0.78f, 0.48f);
        b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Gear(ICanvasResourceCreator rc)
    {
        using var hub = CanvasGeometry.CreateEllipse(rc, 0.50f, 0.50f, 0.14f, 0.14f);
        using var b = new CanvasPathBuilder(rc);
        for (int i = 0; i < 8; i++)
        {
            double a = i / 8.0 * Math.PI * 2;
            b.BeginFigure(0.5f + (float)Math.Cos(a) * 0.22f, 0.5f + (float)Math.Sin(a) * 0.22f);
            b.AddLine(0.5f + (float)Math.Cos(a) * 0.40f, 0.5f + (float)Math.Sin(a) * 0.40f);
            b.EndFigure(CanvasFigureLoop.Open);
        }
        using var teeth = CanvasGeometry.CreatePath(b);
        return CanvasGeometry.CreateGroup(rc, new[] { hub, teeth });
    }

    static CanvasGeometry Chevron(ICanvasResourceCreator rc, bool left)
    {
        float f = left ? 1f : -1f;
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.5f + 0.18f * f, 0.22f);
        b.AddLine(0.5f - 0.18f * f, 0.50f);
        b.AddLine(0.5f + 0.18f * f, 0.78f);
        b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Xmark(ICanvasResourceCreator rc)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(0.26f, 0.26f); b.AddLine(0.74f, 0.74f); b.EndFigure(CanvasFigureLoop.Open);
        b.BeginFigure(0.74f, 0.26f); b.AddLine(0.26f, 0.74f); b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    static CanvasGeometry Wave(ICanvasResourceCreator rc)
    {
        float[] xs = { 0.18f, 0.32f, 0.46f, 0.60f, 0.74f };
        float[] hs = { 0.22f, 0.42f, 0.62f, 0.38f, 0.26f };
        using var b = new CanvasPathBuilder(rc);
        for (int i = 0; i < xs.Length; i++)
        {
            b.BeginFigure(xs[i], 0.5f - hs[i] / 2); b.AddLine(xs[i], 0.5f + hs[i] / 2);
            b.EndFigure(CanvasFigureLoop.Open);
        }
        return CanvasGeometry.CreatePath(b);
    }

    /// <summary>iOS WobblyCircle — two jittered passes (seed=3).</summary>
    public static CanvasGeometry WobblyCircle(ICanvasResourceCreator rc, Vector2 c, float R, ulong seed = 3)
    {
        ulong state = seed + 0x9E3779B97F4A7C15UL;
        using var b = new CanvasPathBuilder(rc);
        for (int pass = 0; pass < 2; pass++)
        {
            const int n = 14;
            for (int i = 0; i <= n; i++)
            {
                double a = i / (double)n * Math.PI * 2 + (pass == 0 ? 0 : 0.3);
                float rr = R * (1 + (float)DoodlePaths.SeededRNG.D(ref state, -0.05, 0.05));
                var pt = new Vector2(c.X + (float)Math.Cos(a) * rr, c.Y + (float)Math.Sin(a) * rr);
                if (i == 0) b.BeginFigure(pt); else b.AddLine(pt);
            }
            b.EndFigure(CanvasFigureLoop.Open);
        }
        return CanvasGeometry.CreatePath(b);
    }

    /// <summary>Draw YMIcon: second pass 0.35 @ (0.6,0.7), optional fill, main stroke.</summary>
    public static void DrawIcon(
        CanvasDrawingSession ds, ICanvasResourceCreator rc, YMGlyph glyph,
        float sizePx, Color ink, float weight = 1.7f, bool filled = false)
    {
        using var geo = Scaled(rc, glyph, sizePx);
        float lw = MathF.Max(1.2f, weight * sizePx / 22f);
        bool doFill = filled || glyph is YMGlyph.Play or YMGlyph.Pause or YMGlyph.HeartFill;
        var style = new CanvasStrokeStyle { StartCap = CanvasCapStyle.Round, EndCap = CanvasCapStyle.Round, LineJoin = CanvasLineJoin.Round };
        var prev = ds.Transform;
        ds.Transform = Matrix3x2.CreateTranslation(0.6f, 0.7f) * prev;
        var dim = Color.FromArgb((byte)(ink.A * 0.35f), ink.R, ink.G, ink.B);
        ds.DrawGeometry(geo, dim, lw * 0.85f, style);
        ds.Transform = prev;
        if (doFill) ds.FillGeometry(geo, ink);
        ds.DrawGeometry(geo, ink, lw, style);
    }

    public static void DrawBrandMark(CanvasDrawingSession ds, ICanvasResourceCreator rc, float sizePx, bool accentSparkle = true)
    {
        float lw = 1.8f * (sizePx / 36f);
        using var ring = WobblyCircle(rc, new Vector2(sizePx / 2f, sizePx / 2f), sizePx * 0.46f, 3);
        var style = new CanvasStrokeStyle { StartCap = CanvasCapStyle.Round, EndCap = CanvasCapStyle.Round };
        ds.DrawGeometry(ring, Color.FromArgb(230, 255, 255, 255), lw, style);
        var prev = ds.Transform;
        ds.Transform = Matrix3x2.CreateTranslation(0.8f, -0.6f)
                     * Matrix3x2.CreateRotation(0.8f * MathF.PI / 180f, new Vector2(sizePx / 2f, sizePx / 2f))
                     * prev;
        ds.DrawGeometry(ring, Color.FromArgb(89, 255, 255, 255), lw * 0.7f, style);
        ds.Transform = prev;
        using var spark = DoodlePaths.Sparkle(rc, new Vector2(sizePx * 0.78f, sizePx * 0.22f), sizePx * 0.16f);
        var sc = accentSparkle ? Color.FromArgb(255, 0xC8, 0xF5, 0x42) : Color.FromArgb(255, 255, 255, 255);
        ds.DrawGeometry(spark, sc, MathF.Max(1.2f, lw * 0.85f), style);
    }
}
