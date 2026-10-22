#include <metal_stdlib>
#include <SwiftUI/SwiftUI_Metal.h>
using namespace metal;

// Signed distance to a capsule / rounded rect (x,y,w,h) with radius r.
static float sdRR(float2 p, float4 R, float r) {
    float2 c = R.xy + R.zw * 0.5;
    float2 q = abs(p - c) - (R.zw * 0.5 - r);
    return length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - r;
}

static float2 lensOffset(float2 p, float4 R, float bezel, float strength) {
    if (R.z <= 0.0) return float2(0);
    float r = min(R.z, R.w) * 0.5;
    float d = sdRR(p, R, r);
    if (d > 0.0 || -d > bezel) return float2(0);
    float e = 0.75;
    float2 n = normalize(float2(sdRR(p + float2(e, 0), R, r) - sdRR(p - float2(e, 0), R, r),
                                sdRR(p + float2(0, e), R, r) - sdRR(p - float2(0, e), R, r)) + 1e-5);
    float t = 1.0 - (-d) / bezel;
    float m = t * t * (3.0 - 2.0 * t);
    return n * m * strength;   // sample outward-in: content near the rim is pulled & magnified
}

// Liquid-glass refraction applied to the content layer under the floating chrome.
// Same math ports 1:1 to Android AGSL RuntimeShader.
[[ stitchable ]] half4 glassLens(float2 pos, SwiftUI::Layer layer, float4 r1, float4 r2, float4 r3, float bezel, float strength) {
    float2 o = lensOffset(pos, r1, bezel, strength) + lensOffset(pos, r2, bezel, strength) + lensOffset(pos, r3, bezel, strength);
    if (all(o == float2(0))) return layer.sample(pos);
    half4 cr = layer.sample(pos + o * 1.00);
    half4 cg = layer.sample(pos + o * 0.92);
    half4 cb = layer.sample(pos + o * 0.84);
    return half4(cr.r, cg.g, cb.b, cg.a);
}

// Specular rim: thin bright edge strongest top-left & bottom-right, like light on curved glass.
[[ stitchable ]] half4 glassRim(float2 pos, half4 color, float2 size, float intensity) {
    float4 R = float4(0, 0, size.x, size.y);
    float r = min(size.x, size.y) * 0.5;
    float d = sdRR(pos, R, r);
    float edge = smoothstep(1.6, 0.0, abs(d + 0.8));
    float inner = smoothstep(10.0, 0.0, -d) * 0.10;
    float e = 0.75;
    float2 n = normalize(float2(sdRR(pos + float2(e, 0), R, r) - sdRR(pos - float2(e, 0), R, r),
                                sdRR(pos + float2(0, e), R, r) - sdRR(pos - float2(0, e), R, r)) + 1e-5);
    float light = abs(dot(n, normalize(float2(-1.0, -1.0))));
    float a = (edge * (0.18 + 0.82 * pow(light, 2.0)) + inner * light) * intensity;
    return half4(a, a, a, a);
}

// Crayon wobble for doodle strokes: small, smooth, time-varying displacement.
[[ stitchable ]] float2 doodleWobble(float2 pos, float t, float amp) {
    float dx = sin(pos.y * 0.045 + t * 1.7) * 0.6 + sin(pos.y * 0.13 + t * 0.9) * 0.4;
    float dy = cos(pos.x * 0.05 + t * 1.3) * 0.6 + sin(pos.x * 0.11 - t * 1.1) * 0.4;
    return pos + float2(dx, dy) * amp * 0.5;
}
