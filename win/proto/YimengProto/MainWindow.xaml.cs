using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
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

using YimengProto.Core;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Imaging;
using System.Threading.Tasks;

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
    private HomePayload? _home;
    private List<CoreTrack> _fm = new();
    private string _status = "连接 Core…";
    private enum SkelTab { Home, Library, Collection, Search, Me }
    private SkelTab _tab = SkelTab.Home;

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
        BuildSideNav();
        _ = LoadCoreAsync();
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
        Activated += (_, _) =>
        {
            AttachGlassBlur();
            if (string.Equals(Environment.GetEnvironmentVariable("YIMENG_OPEN_PLAYER"), "1", StringComparison.Ordinal))
                ShowPlayer();
        };
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
    /// Mini: + Base #0A0A0C ~28% (skel2 more translucent) as a SINGLE layer (XAML Background is Transparent).
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
            // Base #0A0A0C ~28% — single layer (XAML Background is Transparent)
            var baseTint = new ColorSourceEffect { Color = Color.FromArgb(72, 0x0A, 0x0A, 0x0C) };
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

    private void BuildSideNav()
    {
        SideNav.Children.Clear();
        void Add(SkelTab tab, YMGlyph g, string label)
        {
            var row = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 10 };
            var icon = new Microsoft.Graphics.Canvas.UI.Xaml.CanvasControl { Width = 20, Height = 20 };
            icon.Draw += (s, e) =>
            {
                var ink = tab == _tab
                    ? Windows.UI.Color.FromArgb(255, 0xC8, 0xF5, 0x42)
                    : Windows.UI.Color.FromArgb(191, 255, 255, 255);
                YMIconPaths.DrawIcon(e.DrawingSession, s, g, 20, ink, weight: 1.7f);
            };
            row.Children.Add(icon);
            row.Children.Add(new TextBlock
            {
                Text = label,
                Foreground = new SolidColorBrush(tab == _tab
                    ? Windows.UI.Color.FromArgb(255, 0xC8, 0xF5, 0x42)
                    : Windows.UI.Color.FromArgb(191, 255, 255, 255)),
                FontSize = 14,
                FontWeight = tab == _tab ? Microsoft.UI.Text.FontWeights.SemiBold : Microsoft.UI.Text.FontWeights.Normal,
                VerticalAlignment = VerticalAlignment.Center,
            });
            var border = new Border
            {
                CornerRadius = new CornerRadius(10),
                Padding = new Thickness(10, 10, 10, 10),
                Background = tab == _tab
                    ? new SolidColorBrush(Windows.UI.Color.FromArgb(0x40, 0xC8, 0xF5, 0x42))
                    : new SolidColorBrush(Windows.UI.Color.FromArgb(0, 0, 0, 0)),
                Child = row,
            };
            border.PointerPressed += (_, __) => SelectTab(tab);
            SideNav.Children.Add(border);
        }
        Add(SkelTab.Home, YMGlyph.Home, "首页");
        Add(SkelTab.Library, YMGlyph.Library, "曲库");
        Add(SkelTab.Collection, YMGlyph.Collection, "典藏");
        Add(SkelTab.Search, YMGlyph.Search, "搜索");
        Add(SkelTab.Me, YMGlyph.Me, "我的");
    }

    private void SelectTab(SkelTab tab)
    {
        _tab = tab;
        BuildSideNav();
        HomePane.Visibility = tab == SkelTab.Home && PlayerPane.Visibility != Visibility.Visible ? Visibility.Visible : Visibility.Collapsed;
        LibraryPane.Visibility = tab == SkelTab.Library ? Visibility.Visible : Visibility.Collapsed;
        CollectionPane.Visibility = tab == SkelTab.Collection ? Visibility.Visible : Visibility.Collapsed;
        SearchPane.Visibility = tab == SkelTab.Search ? Visibility.Visible : Visibility.Collapsed;
        MePane.Visibility = tab == SkelTab.Me ? Visibility.Visible : Visibility.Collapsed;
        if (PlayerPane.Visibility == Visibility.Visible) return;
        MiniPlayerGlass.Visibility = Visibility.Visible;
        if (tab == SkelTab.Library) RenderLibrary();
        if (tab == SkelTab.Collection) RenderCollection();
        if (tab == SkelTab.Search) _ = RenderSearchAsync();
        if (tab == SkelTab.Me) RenderMe();
    }

    private async Task LoadCoreAsync()
    {
        StatusText.Text = "连接 Core…";
        var account = Path.Combine(AppContext.BaseDirectory, "core-account.env");
        if (!File.Exists(account))
            account = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), "yimeng-win-proto", "core-account.env");
        var ok = await CoreApi.EnsureLoginAsync(account);
        if (!ok)
        {
            _status = $"Core 失败：{CoreApi.LastError}";
            StatusText.Text = _status;
            RenderHomePlaceholder();
            return;
        }
        _home = await CoreApi.FetchHomeAsync();
        _fm = await CoreApi.FetchFmAsync("netease");
        _status = _home == null ? $"首页空：{CoreApi.LastError}" : $"已连接 · {CoreApi.UserLabel}";
        StatusText.Text = _status;
        RenderHome();
        var first = _home?.Sections.Find(s => s.Kind == "tracks" && s.Tracks.Count > 0)?.Tracks[0]
                    ?? _home?.RecentlyPlayed.FirstOrDefault();
        if (first != null)
        {
            MiniTitle.Text = first.Title;
            MiniArtist.Text = first.Artist;
            PlayerTitle.Text = first.Title;
            PlayerArtist.Text = first.Artist;
        }
    }

    private void RenderHomePlaceholder()
    {
        HomeStack.Children.Clear();
        HomeStack.Children.Add(TitleBlock("首页"));
        HomeStack.Children.Add(Muted(_status));
    }

    private void RenderHome()
    {
        HomeStack.Children.Clear();
        HomeStack.Children.Add(TitleBlock("首页"));
        HomeStack.Children.Add(Muted(_status));

        var daily = _home?.Sections.Find(s => s.Id.Contains("daily"));
        var recPl = _home?.Sections.Find(s => s.Id.Contains("playlists") && s.Kind == "playlists" && !s.Id.Contains("mine") && !s.Id.Contains("charts"));
        var yue = daily?.Tracks.FirstOrDefault();

        HomeStack.Children.Add(Section("今日乐签"));
        HomeStack.Children.Add(YueqianTicket(yue));

        if (daily != null && daily.Tracks.Count > 0)
        {
            HomeStack.Children.Add(Section(daily.Title));
            HomeStack.Children.Add(DailyMixCard(daily.Tracks));
            // wide-screen density: cover grid under dated card (Mac v46 multi-column)
            HomeStack.Children.Add(CoverGridTracks(daily.Tracks.Take(12).ToList(), 140));
        }

        HomeStack.Children.Add(Section("网易云音乐 · 私人 FM"));
        HomeStack.Children.Add(FmTile(_fm.FirstOrDefault()));

        if (recPl != null && recPl.Playlists.Count > 0)
        {
            HomeStack.Children.Add(Section(recPl.Title));
            HomeStack.Children.Add(CoverGridPlaylists(recPl.Playlists.Take(12).ToList(), 140));
        }

        if (_home?.RecentlyPlayed.Count > 0)
        {
            HomeStack.Children.Add(Section("最近播放"));
            foreach (var t in _home.RecentlyPlayed.Take(8))
                HomeStack.Children.Add(TrackRow(t));
        }
    }

    private void RenderLibrary()
    {
        LibraryStack.Children.Clear();
        LibraryStack.Children.Add(TitleBlock("曲库"));
        LibraryStack.Children.Add(Muted("平台透传 · 歌单 / 排行"));
        foreach (var sec in _home?.Sections.Where(s => s.Kind == "playlists") ?? Array.Empty<HomeSection>())
        {
            LibraryStack.Children.Add(Section(sec.Title));
            LibraryStack.Children.Add(HScrollPlaylists(sec.Playlists.Take(10).ToList()));
        }
    }

    private void RenderCollection()
    {
        CollectionStack.Children.Clear();
        CollectionStack.Children.Add(TitleBlock("典藏"));
        CollectionStack.Children.Add(Muted("收藏骨架（/favorites）"));
        foreach (var t in _home?.RecentlyPlayed.Take(6) ?? Array.Empty<CoreTrack>())
            CollectionStack.Children.Add(TrackRow(t));
    }

    private async Task RenderSearchAsync()
    {
        SearchStack.Children.Clear();
        SearchStack.Children.Add(TitleBlock("搜索"));
        SearchStack.Children.Add(Muted("查询：梁博"));
        var results = await CoreApi.SearchAsync("梁博");
        foreach (var t in results.Take(20))
            SearchStack.Children.Add(TrackRow(t));
        if (results.Count == 0)
            SearchStack.Children.Add(Muted(CoreApi.LastError ?? "无结果"));
    }

    private void RenderMe()
    {
        MeStack.Children.Clear();
        MeStack.Children.Add(TitleBlock("我的"));
        MeStack.Children.Add(new TextBlock { Text = string.IsNullOrEmpty(CoreApi.UserLabel) ? "未登录" : CoreApi.UserLabel, Foreground = new SolidColorBrush(Colors.White), FontSize = 18, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, Margin = new Thickness(0, 12, 0, 4) });
        MeStack.Children.Add(Muted(_status));
        foreach (var x in new[] { "设置", "音质", "下载", "关于译梦" })
            MeStack.Children.Add(new TextBlock { Text = x, Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(220, 255, 255, 255)), FontSize = 16, Margin = new Thickness(0, 12, 0, 0) });
    }

    private static TextBlock TitleBlock(string s) => new() { Text = s, Foreground = new SolidColorBrush(Colors.White), FontSize = 28, FontWeight = Microsoft.UI.Text.FontWeights.Bold, Margin = new Thickness(0, 0, 0, 8) };
    private static TextBlock Section(string s) => new() { Text = s, Foreground = new SolidColorBrush(Colors.White), FontSize = 17, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, Margin = new Thickness(0, 14, 0, 8) };
    private static TextBlock Muted(string s) => new() { Text = s, Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(120, 255, 255, 255)), FontSize = 12, Margin = new Thickness(0, 0, 0, 8) };

    private Border YueqianTicket(CoreTrack? t)
    {
        var paper = Windows.UI.Color.FromArgb(255, 0xF3, 0xED, 0xE2);
        var ink = Windows.UI.Color.FromArgb(255, 0x2A, 0x24, 0x1C);
        var accent = Windows.UI.Color.FromArgb(255, 0xC4, 0x5C, 0x26);
        var now = DateTime.Now;
        var week = new[] { "周日", "周一", "周二", "周三", "周四", "周五", "周六" };
        var root = new Grid { Padding = new Thickness(0, 14, 0, 14) };
        root.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(78) });
        root.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(14) });
        root.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        root.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(76) });

        var dateCol = new StackPanel { HorizontalAlignment = HorizontalAlignment.Center, Spacing = 0 };
        dateCol.Children.Add(new TextBlock { Text = $"{now.Month}月", Foreground = new SolidColorBrush(accent), FontSize = 12, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, HorizontalAlignment = HorizontalAlignment.Center });
        dateCol.Children.Add(new TextBlock { Text = now.Day.ToString("00"), Foreground = new SolidColorBrush(accent), FontSize = 34, FontWeight = Microsoft.UI.Text.FontWeights.Bold, HorizontalAlignment = HorizontalAlignment.Center });
        dateCol.Children.Add(new TextBlock { Text = week[(int)now.DayOfWeek], Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(140, 0x2A, 0x24, 0x1C)), FontSize = 10, HorizontalAlignment = HorizontalAlignment.Center });
        Grid.SetColumn(dateCol, 0);

        var perf = new Border { Width = 1, Height = 64, VerticalAlignment = VerticalAlignment.Center, Background = new SolidColorBrush(Windows.UI.Color.FromArgb(64, 0x2A, 0x24, 0x1C)) };
        Grid.SetColumn(perf, 1);

        var body = new StackPanel { Spacing = 4, VerticalAlignment = VerticalAlignment.Center };
        body.Children.Add(new TextBlock { Text = "今日乐签", Foreground = new SolidColorBrush(accent), FontSize = 11, FontWeight = Microsoft.UI.Text.FontWeights.Bold });
        body.Children.Add(new TextBlock { Text = t == null ? "打开译梦取今日一签" : $"「{t.Title}」", Foreground = new SolidColorBrush(ink), FontSize = 15, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, TextWrapping = TextWrapping.WrapWholeWords, MaxLines = 2 });
        body.Children.Add(new TextBlock { Text = t == null ? "登录平台后有票根" : $"{t.Title} — {t.Artist}", Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(140, 0x2A, 0x24, 0x1C)), FontSize = 12, TextTrimming = TextTrimming.CharacterEllipsis });
        Grid.SetColumn(body, 2);

        var cover = CoverBorder(t?.CoverUrl, 62, 62, 8, Windows.UI.Color.FromArgb(255, 0xE0, 0xD6, 0xC8));
        cover.Margin = new Thickness(0, 0, 14, 0);
        Grid.SetColumn(cover, 3);

        root.Children.Add(dateCol); root.Children.Add(perf); root.Children.Add(body); root.Children.Add(cover);
        var b = new Border { CornerRadius = new CornerRadius(16), Background = new SolidColorBrush(paper), Child = root, Margin = new Thickness(0, 0, 0, 8) };
        if (t != null) b.PointerPressed += (_, __) => OpenTrack(t);
        return b;
    }

    private Border DailyMixCard(List<CoreTrack> tracks)
    {
        var day = DateTime.Now.Day;
        var row = new Grid { Margin = new Thickness(0, 0, 0, 10) };
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(92) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(14) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(40) });

        var tile = new Grid { Width = 92, Height = 92 };
        tile.Children.Add(CoverBorder(tracks.FirstOrDefault()?.CoverUrl, 92, 92, 16, Windows.UI.Color.FromArgb(255, 0x6B, 0x4B, 0xD8)));
        tile.Children.Add(new Border { Background = new SolidColorBrush(Windows.UI.Color.FromArgb(100, 0, 0, 0)), CornerRadius = new CornerRadius(16) });
        tile.Children.Add(new TextBlock { Text = day.ToString(), Foreground = new SolidColorBrush(Colors.White), FontSize = 40, FontWeight = Microsoft.UI.Text.FontWeights.Bold, HorizontalAlignment = HorizontalAlignment.Center, VerticalAlignment = VerticalAlignment.Center });
        Grid.SetColumn(tile, 0);

        var col = new StackPanel { VerticalAlignment = VerticalAlignment.Center, Spacing = 4 };
        col.Children.Add(new TextBlock { Text = $"{tracks.Count} 首 · 按账号", Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(160, 255, 255, 255)), FontSize = 13 });
        foreach (var s in tracks.Take(2))
            col.Children.Add(new TextBlock { Text = $"{s.Title} — {s.Artist}", Foreground = new SolidColorBrush(Colors.White), FontSize = 14, TextTrimming = TextTrimming.CharacterEllipsis });
        Grid.SetColumn(col, 2);

        var play = new TextBlock { Text = "▶", Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(255, 0xC8, 0xF5, 0x42)), FontSize = 22, VerticalAlignment = VerticalAlignment.Center, HorizontalAlignment = HorizontalAlignment.Center };
        Grid.SetColumn(play, 3);

        row.Children.Add(tile); row.Children.Add(col); row.Children.Add(play);
        var b = new Border { CornerRadius = new CornerRadius(16), Padding = new Thickness(14), Background = new SolidColorBrush(Windows.UI.Color.FromArgb(0x14, 255, 255, 255)), Child = row };
        if (tracks.Count > 0) b.PointerPressed += (_, __) => OpenTrack(tracks[0]);
        return b;
    }

    private Border CoverBorder(string? url, double w, double h, double radius, Windows.UI.Color fallback)
    {
        var b = new Border { Width = w, Height = h, CornerRadius = new CornerRadius(radius), Background = new SolidColorBrush(fallback) };
        if (!string.IsNullOrWhiteSpace(url))
        {
            try
            {
                var img = new Image
                {
                    Stretch = Stretch.UniformToFill,
                    Source = new BitmapImage(new Uri(url)),
                };
                b.Child = img;
            }
            catch { }
        }
        return b;
    }

    private VariableSizedWrapGrid CoverGridTracks(List<CoreTrack> tracks, double size)
    {
        var grid = new VariableSizedWrapGrid { Orientation = Orientation.Horizontal, ItemWidth = size + 12, ItemHeight = size + 48, MaximumRowsOrColumns = 6, HorizontalAlignment = HorizontalAlignment.Left };
        foreach (var t in tracks)
        {
            var card = new StackPanel { Width = size, Spacing = 6, Margin = new Thickness(0, 0, 12, 12) };
            card.Children.Add(CoverBorder(t.CoverUrl, size, size, 12, Windows.UI.Color.FromArgb(255, 0x3D, 0x2E, 0xFF)));
            card.Children.Add(new TextBlock { Text = t.Title, Foreground = new SolidColorBrush(Colors.White), FontSize = 13, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, TextWrapping = TextWrapping.WrapWholeWords, MaxLines = 2 });
            card.Children.Add(new TextBlock { Text = t.Artist, Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(128, 255, 255, 255)), FontSize = 11 });
            var wrap = new Border { Child = card };
            wrap.PointerPressed += (_, __) => OpenTrack(t);
            grid.Children.Add(wrap);
        }
        return grid;
    }

    private VariableSizedWrapGrid CoverGridPlaylists(List<CorePlaylist> pls, double size)
    {
        var grid = new VariableSizedWrapGrid { Orientation = Orientation.Horizontal, ItemWidth = size + 12, ItemHeight = size + 40, MaximumRowsOrColumns = 6, HorizontalAlignment = HorizontalAlignment.Left };
        foreach (var p in pls)
        {
            var card = new StackPanel { Width = size, Spacing = 6, Margin = new Thickness(0, 0, 12, 12) };
            card.Children.Add(CoverBorder(p.CoverUrl, size, size, 12, Windows.UI.Color.FromArgb(255, 0x00, 0xB0, 0xA0)));
            card.Children.Add(new TextBlock { Text = p.Name, Foreground = new SolidColorBrush(Colors.White), FontSize = 13, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, TextWrapping = TextWrapping.WrapWholeWords, MaxLines = 2 });
            grid.Children.Add(card);
        }
        return grid;
    }

    private Border FmTile(CoreTrack? t)
    {
        var b = new Border { CornerRadius = new CornerRadius(16), Padding = new Thickness(14), Background = new SolidColorBrush(Windows.UI.Color.FromArgb(0x14, 255, 255, 255)) };
        var row = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 12 };
        row.Children.Add(CoverBorder(t?.CoverUrl, 64, 64, 12, Windows.UI.Color.FromArgb(255, 0x6B, 0x4B, 0xD8)));
        var col = new StackPanel { VerticalAlignment = VerticalAlignment.Center, Spacing = 4 };
        col.Children.Add(new TextBlock { Text = "私人 FM", Foreground = new SolidColorBrush(Colors.White), FontSize = 16, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        col.Children.Add(new TextBlock { Text = t == null ? "点按开始电台" : $"{t.Title} · {t.Artist}", Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(160, 255, 255, 255)), FontSize = 12 });
        row.Children.Add(col);
        b.Child = row;
        if (t != null) b.PointerPressed += (_, __) => OpenTrack(t);
        return b;
    }

    private ScrollViewer HScrollTracks(List<CoreTrack> tracks)
    {
        var sp = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 12 };
        foreach (var t in tracks)
        {
            var card = new StackPanel { Width = 118, Spacing = 6 };
            card.Children.Add(new Border { Width = 118, Height = 118, CornerRadius = new CornerRadius(12), Background = new SolidColorBrush(Windows.UI.Color.FromArgb(255, 0x3D, 0x2E, 0xFF)) });
            card.Children.Add(new TextBlock { Text = t.Title, Foreground = new SolidColorBrush(Colors.White), FontSize = 13, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, TextWrapping = TextWrapping.WrapWholeWords, MaxLines = 2 });
            card.Children.Add(new TextBlock { Text = t.Artist, Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(128, 255, 255, 255)), FontSize = 11 });
            var wrap = new Border { Child = card };
            wrap.PointerPressed += (_, __) => OpenTrack(t);
            sp.Children.Add(wrap);
        }
        return new ScrollViewer { HorizontalScrollBarVisibility = ScrollBarVisibility.Hidden, VerticalScrollBarVisibility = ScrollBarVisibility.Disabled, Content = sp, Margin = new Thickness(0, 0, 0, 8) };
    }

    private ScrollViewer HScrollPlaylists(List<CorePlaylist> pls)
    {
        var sp = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 12 };
        foreach (var p in pls)
        {
            var card = new StackPanel { Width = 118, Spacing = 6 };
            card.Children.Add(new Border { Width = 118, Height = 118, CornerRadius = new CornerRadius(12), Background = new SolidColorBrush(Windows.UI.Color.FromArgb(255, 0x00, 0xB0, 0xA0)) });
            card.Children.Add(new TextBlock { Text = p.Name, Foreground = new SolidColorBrush(Colors.White), FontSize = 13, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, TextWrapping = TextWrapping.WrapWholeWords, MaxLines = 2 });
            sp.Children.Add(card);
        }
        return new ScrollViewer { HorizontalScrollBarVisibility = ScrollBarVisibility.Hidden, VerticalScrollBarVisibility = ScrollBarVisibility.Disabled, Content = sp, Margin = new Thickness(0, 0, 0, 8) };
    }

    private Border TrackRow(CoreTrack t)
    {
        var row = new Grid { Margin = new Thickness(0, 4, 0, 4) };
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(48) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(12) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var cover = CoverBorder(t.CoverUrl, 48, 48, 8, Windows.UI.Color.FromArgb(255, 0xFF, 0x7A, 0x1A));
        Grid.SetColumn(cover, 0);
        var col = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        col.Children.Add(new TextBlock { Text = t.Title, Foreground = new SolidColorBrush(Colors.White), FontSize = 15, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        col.Children.Add(new TextBlock { Text = t.Artist, Foreground = new SolidColorBrush(Windows.UI.Color.FromArgb(140, 255, 255, 255)), FontSize = 12 });
        Grid.SetColumn(col, 2);
        row.Children.Add(cover); row.Children.Add(col);
        var b = new Border { Child = row, Padding = new Thickness(4) };
        b.PointerPressed += (_, __) => OpenTrack(t);
        return b;
    }

    private void OpenTrack(CoreTrack t)
    {
        MiniTitle.Text = t.Title;
        MiniArtist.Text = t.Artist;
        PlayerTitle.Text = t.Title;
        PlayerArtist.Text = t.Artist;
        ShowPlayer();
    }

    private void OpenPlayer_Click(object sender, PointerRoutedEventArgs e) => ShowPlayer();
    private void ClosePlayer_Click(object sender, PointerRoutedEventArgs e) => ShowHome();

    private void ShowHome()
    {
        PlayerPane.Visibility = Visibility.Collapsed;
        MiniPlayerGlass.Visibility = Visibility.Visible;
        SelectTab(_tab == SkelTab.Home ? SkelTab.Home : _tab);
        if (_tab == SkelTab.Home) HomePane.Visibility = Visibility.Visible;
    }

    private void ShowPlayer()
    {
        HomePane.Visibility = Visibility.Collapsed;
        LibraryPane.Visibility = Visibility.Collapsed;
        CollectionPane.Visibility = Visibility.Collapsed;
        SearchPane.Visibility = Visibility.Collapsed;
        MePane.Visibility = Visibility.Collapsed;
        PlayerPane.Visibility = Visibility.Visible;
        MiniPlayerGlass.Visibility = Visibility.Collapsed; // iOS: hide mini on full player
        _drawn = 0f; _t = 0f;
        DisposeMotif();
        DoodleCanvas.Invalidate();
    }

    private void GlyphBack_Draw(CanvasControl sender, CanvasDrawEventArgs args)
        => DrawGlyph(sender, args, YMGlyph.Back, ink: Color.FromArgb(204, 255, 255, 255));

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


    private static void DrawGlyph(CanvasControl sender, CanvasDrawEventArgs args, YMGlyph g, bool filled = false, Color? ink = null)
    {
        var ds = args.DrawingSession;
        float s = (float)Math.Min(sender.ActualWidth, sender.ActualHeight);
        if (s < 1) return;
        YMIconPaths.DrawIcon(ds, sender, g, s, ink ?? Colors.White, weight: 1.7f, filled: filled);
    }
}
