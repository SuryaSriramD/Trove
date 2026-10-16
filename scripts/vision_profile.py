"""Pinned, private-source geometry adaptation. Never modifies cached upstream."""
import hashlib
import json
from pathlib import Path, PurePosixPath
import tarfile

PINNED_RUNTIME = "d2e54583c7452353eb35d40431281f6ee984332f"
PINNED_PROFILE_SHA256 = "d3288e6d3d3c197c5e31e6d836427a93562315d679519b73eda799fbbef9be2b"
RUNTIME_HASHES = {'tools/mtmd/mtmd.h': 'b0f1ba884e8688980789f0547147d2b7330d65772ebac8f36c0dbb8ece7cf7d0', 'tools/mtmd/mtmd.cpp': 'b5f5660650d5abd1b8e23b6e4246f33eedc73b79fc904c2ff521796060bd56bb', 'tools/mtmd/clip.h': '07b67b04559cd203e389efa9a0399e61abf2a134d8ba14b194b12adaf3aba21a', 'tools/mtmd/clip.cpp': 'cf04abaa1765cd735478de2e5124ee7442c819bf89f18c49f80428ee03fca7b5'}


def replace_once(text, before, after):
    if text.count(before) != 1:
        raise ValueError("Pinned vision runtime adaptation no longer matches")
    return text.replace(before, after, 1)


def adapt_runtime(relative, original):
    if relative not in RUNTIME_HASHES:
        return original
    if hashlib.sha256(original).hexdigest() != RUNTIME_HASHES[relative]:
        raise ValueError("Unverified vision runtime source: " + relative)
    text = original.decode()
    if relative == "tools/mtmd/mtmd.h":
        text = replace_once(text, "    int image_max_tokens; // maximum number of tokens for image input (default: read from metadata)", "    int image_max_tokens; // maximum number of tokens for image input (default: read from metadata)\n    int image_longest_edge; // Trove: exact verified profile only; zero preserves upstream defaults")
    elif relative == "tools/mtmd/clip.h":
        text = replace_once(text, "    int image_max_tokens;", "    int image_max_tokens;\n    int image_longest_edge;")
    elif relative == "tools/mtmd/mtmd.cpp":
        text = replace_once(text, "        /* image_max_tokens  */ -1,", "        /* image_max_tokens  */ -1,\n        /* image_longest_edge */ 0,")
        text = replace_once(text, "            /* image_max_tokens  */ ctx_params.image_max_tokens,", "            /* image_max_tokens  */ ctx_params.image_max_tokens,\n            /* image_longest_edge */ ctx_params.image_longest_edge,")
    elif relative == "tools/mtmd/clip.cpp":
        old = "            loader.load_hparams(ctx_vision->model, CLIP_MODALITY_VISION);"
        new = old + """
            // Trove's code-pinned bounded profile restores absent geometry only.
            // Refuse a different projector shape, preexisting geometry or limits.
            if (ctx_params.image_longest_edge != 0) {
                auto & h = ctx_vision->model.hparams;
                if (ctx_params.image_longest_edge != 2048 || ctx_vision->model.proj_type != PROJECTOR_TYPE_IDEFICS3 || h.image_size != 512 || h.patch_size != 16 || h.n_merge != 4 || h.image_longest_edge != 0 || h.image_min_pixels > 0 || h.image_max_pixels > 0 || h.image_resize_algo != RESIZE_ALGO_LANCZOS) {
                    throw std::runtime_error("incompatible bounded image preprocessing profile");
                }
                for (int c = 0; c < 3; ++c) {
                    if (h.image_mean[c] != 0.5f || h.image_std[c] != 0.5f) {
                        throw std::runtime_error("incompatible bounded image color profile");
                    }
                }
                h.image_longest_edge = ctx_params.image_longest_edge;
            }"""
        text = replace_once(text, old, new)
    return text.encode()


def read_profile(path):
    raw = Path(path).read_bytes()
    if hashlib.sha256(raw).hexdigest() != PINNED_PROFILE_SHA256:
        raise ValueError("Vision profile changed without reviewed build binding")
    p = json.loads(raw)
    if p['runtimeCommit'] != PINNED_RUNTIME or p['geometry']['longestEdge'] != 2048:
        raise ValueError("Incompatible vision profile")
    return p


def profile_header(profile):
    # A generated build input, derived only from code-pinned JSON. No model cache
    # metadata, environment selection, or request can supply arbitrary geometry.
    constants = {'ID':profile['id'], 'HASH':PINNED_PROFILE_SHA256, 'MODEL':profile['modelSHA256'], 'PROJECTOR':profile['projectorSHA256']}
    lines = ['#pragma once'] + ['inline constexpr const char * VISION_PROFILE_' + key + ' = ' + json.dumps(value) + ';' for key, value in constants.items()]
    return '\n'.join(lines) + '\ninline constexpr int VISION_PROFILE_LONGEST_EDGE = 2048;\n'


def stage_profile_source(archive, pin, destination):
    """Reconstruct from verified archive, and verify every reused staged byte."""
    archive, destination = Path(archive), Path(destination)
    if pin['commit'] != PINNED_RUNTIME or hashlib.sha256(archive.read_bytes()).hexdigest() != pin['sha256']:
        raise ValueError("Unverified vision archive")
    if destination.is_symlink():
        raise ValueError("Vision source stage cannot be a symlink")
    existing = destination.exists()
    destination.mkdir(parents=True, exist_ok=True)
    expected = set()
    with tarfile.open(archive) as tar:
        for member in tar:
            path = PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not path.parts or path.parts[0] != pin['sourceDirectory']:
                raise ValueError("Invalid pinned archive path")
            if member.isdir():
                continue
            if not member.isfile():
                raise ValueError("Unsupported pinned archive entry")
            relative = str(PurePosixPath(*path.parts[1:]))
            expected.add(relative)
            raw = adapt_runtime(relative, tar.extractfile(member).read())
            target = destination / relative
            if any((destination / parent).is_symlink() for parent in Path(relative).parents) or target.is_symlink():
                raise ValueError("Vision source stage contains symlink")
            if existing:
                if not target.is_file() or target.read_bytes() != raw:
                    raise ValueError("Vision source stage changed: " + relative)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(raw)
    actual = {str(p.relative_to(destination)) for p in destination.rglob('*') if p.is_file() or p.is_symlink()}
    if actual != expected or not set(RUNTIME_HASHES).issubset(expected):
        raise ValueError("Vision source stage has unexpected or missing files")
    return destination
