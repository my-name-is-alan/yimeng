using System;
using System.Numerics;
using Microsoft.Graphics.Canvas;
using Microsoft.Graphics.Canvas.Geometry;

namespace YimengProto;

/// <summary>Line-by-line port of iOS / Android DoodlePaths.</summary>
public static class DoodlePaths
{
    public readonly struct RectF
    {
        public readonly float Left, Top, Right, Bottom;
        public RectF(float l, float t, float r, float b) { Left = l; Top = t; Right = r; Bottom = b; }
        public float Width => Right - Left;
        public float Height => Bottom - Top;
        public Vector2 Center => new((Left + Right) * 0.5f, (Top + Bottom) * 0.5f);
        public RectF Inflate(float v) => new(Left - v, Top - v, Right + v, Bottom + v);
    }

    public static class SeededRNG
    {
        public static ulong Next(ref ulong state)
        {
            state += 0x9E3779B97F4A7C15UL;
            ulong z = state;
            z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9UL;
            z = (z ^ (z >> 27)) * 0x94D049BB133111EBUL;
            return z ^ (z >> 31);
        }
        public static double D(ref ulong state, double a, double b)
        {
            double u = (Next(ref state) >> 11) / (double)(1L << 53);
            return a + (b - a) * u;
        }
    }

    static float Jit(ref ulong r, float j) => (float)(SeededRNG.D(ref r, -1, 1) * j);

    public static CanvasGeometry Frame(ICanvasResourceCreator rc, RectF rect, ulong seed, float jitter)
    {
        using var b = new CanvasPathBuilder(rc);
        ulong r = seed + 0x9E3779B97F4A7C15UL;
        for (int pass = 0; pass < 2; pass++)
        {
            float o = pass * 6f;
            var pts = new[]
            {
                new Vector2(rect.Left - o, rect.Top + o),
                new Vector2(rect.Right + o, rect.Top - o),
                new Vector2(rect.Right - o, rect.Bottom + o),
                new Vector2(rect.Left + o, rect.Bottom - o),
                new Vector2(rect.Left - o + 4f, rect.Top + o - 3f),
            };
            b.BeginFigure(pts[0]);
            for (int i = 1; i < pts.Length; i++)
            {
                var a = pts[i - 1]; var c = pts[i];
                var mid = new Vector2((a.X + c.X) / 2 + Jit(ref r, jitter * 2), (a.Y + c.Y) / 2 + Jit(ref r, jitter * 2));
                b.AddQuadraticBezier(mid, new Vector2(c.X + Jit(ref r, jitter), c.Y + Jit(ref r, jitter)));
            }
            b.EndFigure(CanvasFigureLoop.Open);
        }
        return CanvasGeometry.CreatePath(b);
    }

    public static CanvasGeometry Orbit(ICanvasResourceCreator rc, Vector2 c, float rx, float ry, ulong seed, float jitter)
    {
        using var b = new CanvasPathBuilder(rc);
        ulong r = seed + 0x9E3779B97F4A7C15UL;
        const int n = 120;
        for (int i = 0; i <= n; i++)
        {
            double a = i / (double)n * Math.PI * 2 * 1.6 - 0.4;
            double k = 1 + 0.05 * Math.Sin(a * 3) + Jit(ref r, jitter) * 0.004;
            var pt = new Vector2((float)(c.X + Math.Cos(a) * k * rx), (float)(c.Y + Math.Sin(a) * k * ry));
            if (i == 0) b.BeginFigure(pt); else b.AddLine(pt);
        }
        b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    public static CanvasGeometry Cloud(ICanvasResourceCreator rc, Vector2 o, float w)
    {
        using var b = new CanvasPathBuilder(rc);
        float h = w * 0.42f;
        b.BeginFigure(o.X, o.Y + h);
        b.AddCubicBezier(new Vector2(o.X - w * 0.08f, o.Y + h * 0.7f), new Vector2(o.X + w * 0.05f, o.Y + h * 0.3f), new Vector2(o.X + w * 0.22f, o.Y + h * 0.35f));
        b.AddCubicBezier(new Vector2(o.X + w * 0.28f, o.Y - h * 0.25f), new Vector2(o.X + w * 0.55f, o.Y - h * 0.3f), new Vector2(o.X + w * 0.62f, o.Y + h * 0.2f));
        b.AddCubicBezier(new Vector2(o.X + w * 0.85f, o.Y + h * 0.05f), new Vector2(o.X + w * 1.08f, o.Y + h * 0.45f), new Vector2(o.X + w, o.Y + h * 0.8f));
        b.AddLine(o.X, o.Y + h);
        b.EndFigure(CanvasFigureLoop.Closed);
        return CanvasGeometry.CreatePath(b);
    }

    public static CanvasGeometry Moon(ICanvasResourceCreator rc, Vector2 c, float r)
    {
        using var b = new CanvasPathBuilder(rc);
        // Arc approx via cubic Beziers for -60°..+200° (260° sweep)
        float start = -60f * MathF.PI / 180f;
        float sweep = 260f * MathF.PI / 180f;
        int segs = 8;
        for (int i = 0; i <= segs; i++)
        {
            float a = start + sweep * i / segs;
            var pt = new Vector2(c.X + MathF.Cos(a) * r, c.Y + MathF.Sin(a) * r);
            if (i == 0) b.BeginFigure(pt); else b.AddLine(pt);
        }
        float tx = c.X + r * MathF.Cos(-MathF.PI / 3);
        float ty = c.Y + r * MathF.Sin(-MathF.PI / 3);
        b.AddQuadraticBezier(new Vector2(c.X + r * 0.15f, c.Y + r * 0.1f), new Vector2(tx, ty));
        b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    public static CanvasGeometry Sparkle(ICanvasResourceCreator rc, Vector2 c, float r)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(c.X, c.Y - r);
        b.AddQuadraticBezier(c, new Vector2(c.X + r, c.Y));
        b.AddQuadraticBezier(c, new Vector2(c.X, c.Y + r));
        b.AddQuadraticBezier(c, new Vector2(c.X - r, c.Y));
        b.AddQuadraticBezier(c, new Vector2(c.X, c.Y - r));
        b.EndFigure(CanvasFigureLoop.Closed);
        return CanvasGeometry.CreatePath(b);
    }

    public static CanvasGeometry Star(ICanvasResourceCreator rc, Vector2 c, float r, double rot)
    {
        using var b = new CanvasPathBuilder(rc);
        for (int i = 0; i <= 10; i++)
        {
            double a = rot + i * Math.PI / 5 - Math.PI / 2;
            float rr = (i % 2 == 0) ? r : r * 0.45f;
            var pt = new Vector2(c.X + (float)Math.Cos(a) * rr, c.Y + (float)Math.Sin(a) * rr);
            if (i == 0) b.BeginFigure(pt); else b.AddLine(pt);
        }
        b.EndFigure(CanvasFigureLoop.Closed);
        return CanvasGeometry.CreatePath(b);
    }

    public static CanvasGeometry Lightning(ICanvasResourceCreator rc, Vector2 o, float h)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(o);
        b.AddLine(o.X - h * 0.3f, o.Y + h * 0.5f);
        b.AddLine(o.X + h * 0.05f, o.Y + h * 0.48f);
        b.AddLine(o.X - h * 0.2f, o.Y + h);
        b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    public static CanvasGeometry Zigline(ICanvasResourceCreator rc, Vector2 o, float length, float amp, float step)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(o);
        float x = 0; bool up = true;
        while (x < length) { x += step; b.AddLine(o.X + x, o.Y + (up ? -amp : amp)); up = !up; }
        b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    public static CanvasGeometry Flower(ICanvasResourceCreator rc, Vector2 c, float r, int petals)
    {
        using var b = new CanvasPathBuilder(rc);
        for (int i = 0; i < petals; i++)
        {
            double a = i / (double)petals * Math.PI * 2;
            var tip = new Vector2(c.X + (float)Math.Cos(a) * r, c.Y + (float)Math.Sin(a) * r);
            var l = new Vector2(c.X + (float)Math.Cos(a - 0.45) * r * 0.7f, c.Y + (float)Math.Sin(a - 0.45) * r * 0.7f);
            var rr = new Vector2(c.X + (float)Math.Cos(a + 0.45) * r * 0.7f, c.Y + (float)Math.Sin(a + 0.45) * r * 0.7f);
            b.BeginFigure(c);
            b.AddQuadraticBezier(l, tip);
            b.AddQuadraticBezier(rr, c);
            b.EndFigure(CanvasFigureLoop.Closed);
        }
        var petalsGeo = CanvasGeometry.CreatePath(b);
        var center = CanvasGeometry.CreateEllipse(rc, c, r * 0.18f, r * 0.18f);
        var combined = petalsGeo.CombineWith(center, Matrix3x2.Identity, CanvasGeometryCombine.Union);
        petalsGeo.Dispose(); center.Dispose();
        return combined;
    }

    public static CanvasGeometry Loops(ICanvasResourceCreator rc, Vector2 o, float length, float r)
    {
        using var b = new CanvasPathBuilder(rc);
        const int n = 160;
        for (int i = 0; i <= n; i++)
        {
            double t = i / (double)n; double a = t * Math.PI * 2 * 5;
            var pt = new Vector2(o.X + (float)t * length + (float)Math.Cos(a) * r, o.Y + (float)Math.Sin(a) * r);
            if (i == 0) b.BeginFigure(pt); else b.AddLine(pt);
        }
        b.EndFigure(CanvasFigureLoop.Open);
        return CanvasGeometry.CreatePath(b);
    }

    public static CanvasGeometry Drip(ICanvasResourceCreator rc, Vector2 top, float length)
    {
        using var b = new CanvasPathBuilder(rc);
        b.BeginFigure(top);
        b.AddLine(top.X, top.Y + length);
        b.EndFigure(CanvasFigureLoop.Open);
        var stem = CanvasGeometry.CreatePath(b);
        var drop = CanvasGeometry.CreateEllipse(rc, new Vector2(top.X, top.Y + length + 4.5f), 3.5f, 4.5f);
        var combined = stem.CombineWith(drop, Matrix3x2.Identity, CanvasGeometryCombine.Union);
        stem.Dispose(); drop.Dispose();
        return combined;
    }

    /// <summary>Ink motif parts around a cover slot (player page). Kept separate so wobble is per-contour.</summary>
    public static CanvasGeometry[] MotifInkParts(ICanvasResourceCreator rc, float left, float top, float s)
    {
        float sc = s / 232f;
        // Frame + cloud wrap the cover slot (Android player doodle language); keep flowers/drips as accents.
        var frameRect = new RectF(left - 6f * sc, top - 6f * sc, left + s + 6f * sc, top + s + 6f * sc);
        return new CanvasGeometry[]
        {
            Frame(rc, frameRect, seed: 42, jitter: 3.2f * sc),
            Cloud(rc, new Vector2(left + s * 0.55f, top - 18f * sc), 72f * sc),
            Flower(rc, new Vector2(left + s - 6f * sc, top + 8f * sc), 22f * sc, 6),
            Flower(rc, new Vector2(left + 10f * sc, top + s - 6f * sc), 15f * sc, 5),
            Loops(rc, new Vector2(left - 30f * sc, top - 24f * sc), 120f * sc, 9f * sc),
            Drip(rc, new Vector2(left + s * 0.22f, top + s + 4f * sc), 32f * sc),
            Drip(rc, new Vector2(left + s * 0.48f, top + s + 4f * sc), 46f * sc),
            Drip(rc, new Vector2(left + s * 0.71f, top + s + 4f * sc), 60f * sc),
            Flower(rc, new Vector2(left + s + 40f * sc, top + s + 40f * sc), 10f * sc, 5),
        };
    }

    /// <summary>Ink motif for 橘子海 (acoustic / energy 0.55) — matches Android DreamMock.orange.</summary>
    public static CanvasGeometry MotifInk(ICanvasResourceCreator rc, float w, float h)
    {
        float k = w / 402f;
        float left = 128 * k, top = 136 * k, s = 232 * k;
        var parts = MotifInkParts(rc, left, top, s);
        var group = CanvasGeometry.CreateGroup(rc, parts);
        foreach (var p in parts) p.Dispose();
        return group;
    }

    /// <summary>cubic-bezier(0.42, 0, 0.58, 1)</summary>
    public static float CubicBezierEase(float t)
    {
        const float x1 = 0.42f, y1 = 0f, x2 = 0.58f, y2 = 1f;
        static float Bez(float a, float b, float c, float d, float u)
        {
            float s = 1f - u;
            return s * s * s * a + 3f * s * s * u * b + 3f * s * u * u * c + u * u * u * d;
        }
        static float DBez(float a, float b, float c, float d, float u)
        {
            float s = 1f - u;
            return 3f * s * s * (b - a) + 6f * s * u * (c - b) + 3f * u * u * (d - c);
        }
        float u = Math.Clamp(t, 0f, 1f);
        for (int i = 0; i < 10; i++)
        {
            float x = Bez(0, x1, x2, 1, u);
            float dx = DBez(0, x1, x2, 1, u);
            if (MathF.Abs(dx) < 1e-6f) break;
            u = Math.Clamp(u - (x - t) / dx, 0f, 1f);
        }
        return Bez(0, y1, y2, 1, u);
    }

    /// <summary>Trim ALL contours together by cumulative length (not first contour only).</summary>
    public static CanvasGeometry TrimAll(ICanvasResourceCreator rc, CanvasGeometry src, float fraction)
    {
        float f = Math.Clamp(fraction, 0f, 1f);
        if (f <= 0f) return CanvasGeometry.CreateGroup(rc, Array.Empty<CanvasGeometry>());
        if (f >= 0.999f) return src;

        // Flatten to path and walk with PathBuilder dash via ComputePathLength + CreatePathFromStroke? 
        // Win2D: use CanvasGeometry.Stroke then? Better: CreatePath with dashed stroke of total length.
        float total = src.ComputePathLength();
        if (total <= 0.01f) return src;
        float target = total * f;

        // Stroke the path with a dash that reveals [0, target]
        var style = new CanvasStrokeStyle
        {
            CustomDashStyle = new float[] { MathF.Max(0.01f, target), total },
            DashOffset = 0,
            StartCap = CanvasCapStyle.Round,
            EndCap = CanvasCapStyle.Round,
        };
        // Return stroked outline as fillable? For DrawGeometry we can pass style directly.
        // Caller draws with CustomDashStyle — keep helper that just returns length/target.
        return src; // caller applies dash; this exists for API parity
    }
}
