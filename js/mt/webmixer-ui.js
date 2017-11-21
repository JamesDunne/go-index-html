(function webpackUniversalModuleDefinition(root, factory) {
	if(typeof exports === 'object' && typeof module === 'object')
		module.exports = factory(require("webmixer"));
	else if(typeof define === 'function' && define.amd)
		define(["webmixer"], factory);
	else if(typeof exports === 'object')
		exports["WebmixerUI"] = factory(require("webmixer"));
	else
		root["WebmixerUI"] = factory(root["Webmixer"]);
})(this, function(__WEBPACK_EXTERNAL_MODULE_1__) {
return /******/ (function(modules) { // webpackBootstrap
/******/ 	// The module cache
/******/ 	var installedModules = {};
/******/
/******/ 	// The require function
/******/ 	function __webpack_require__(moduleId) {
/******/
/******/ 		// Check if module is in cache
/******/ 		if(installedModules[moduleId]) {
/******/ 			return installedModules[moduleId].exports;
/******/ 		}
/******/ 		// Create a new module (and put it into the cache)
/******/ 		var module = installedModules[moduleId] = {
/******/ 			i: moduleId,
/******/ 			l: false,
/******/ 			exports: {}
/******/ 		};
/******/
/******/ 		// Execute the module function
/******/ 		modules[moduleId].call(module.exports, module, module.exports, __webpack_require__);
/******/
/******/ 		// Flag the module as loaded
/******/ 		module.l = true;
/******/
/******/ 		// Return the exports of the module
/******/ 		return module.exports;
/******/ 	}
/******/
/******/
/******/ 	// expose the modules object (__webpack_modules__)
/******/ 	__webpack_require__.m = modules;
/******/
/******/ 	// expose the module cache
/******/ 	__webpack_require__.c = installedModules;
/******/
/******/ 	// define getter function for harmony exports
/******/ 	__webpack_require__.d = function(exports, name, getter) {
/******/ 		if(!__webpack_require__.o(exports, name)) {
/******/ 			Object.defineProperty(exports, name, {
/******/ 				configurable: false,
/******/ 				enumerable: true,
/******/ 				get: getter
/******/ 			});
/******/ 		}
/******/ 	};
/******/
/******/ 	// getDefaultExport function for compatibility with non-harmony modules
/******/ 	__webpack_require__.n = function(module) {
/******/ 		var getter = module && module.__esModule ?
/******/ 			function getDefault() { return module['default']; } :
/******/ 			function getModuleExports() { return module; };
/******/ 		__webpack_require__.d(getter, 'a', getter);
/******/ 		return getter;
/******/ 	};
/******/
/******/ 	// Object.prototype.hasOwnProperty.call
/******/ 	__webpack_require__.o = function(object, property) { return Object.prototype.hasOwnProperty.call(object, property); };
/******/
/******/ 	// __webpack_public_path__
/******/ 	__webpack_require__.p = "";
/******/
/******/ 	// Load entry module and return exports
/******/ 	return __webpack_require__(__webpack_require__.s = 0);
/******/ })
/************************************************************************/
/******/ ([
/* 0 */
/***/ (function(module, exports, __webpack_require__) {

var __WEBPACK_AMD_DEFINE_ARRAY__, __WEBPACK_AMD_DEFINE_RESULT__;!(__WEBPACK_AMD_DEFINE_ARRAY__ = [__webpack_require__, exports, __webpack_require__(1)], __WEBPACK_AMD_DEFINE_RESULT__ = function (require, exports, webmixer_1) {
    "use strict";
    Object.defineProperty(exports, "__esModule", { value: true });
    // Define maximum gain at the top of the fader range [0..1]:
    const faderMaxGain = webmixer_1.dB_to_gain(12);
    function gain_to_fader(gain) {
        let fader = Math.pow((6.0 * Math.log(gain) / Math.log(2.0) + 192.0) / 198.0, 8.0);
        return fader;
    }
    // Convert from dB to fader range [0..1]:
    function dB_to_fader(dB) {
        if (dB == -Infinity)
            return 0.0;
        let gain = webmixer_1.dB_to_gain(dB) * 2.0 / faderMaxGain;
        return gain_to_fader(gain);
    }
    // Define a zero-value on the fader [0..1] scale:
    const faderZero = dB_to_fader(0);
    // Convert from fader range [0..1] to dB:
    function fader_to_dB(fader) {
        if (fader == 0.0)
            return -Infinity;
        if (Math.abs(fader - faderZero) < 1e-6)
            return 0;
        let gain = Math.exp(((Math.pow(fader, 1.0 / 8.0) * 198.0) - 192.0) / 6.0 * Math.log(2.0)) * faderMaxGain / 2.0;
        let dB = webmixer_1.gain_to_dB(gain);
        return dB;
    }
    function withExactDigits(value, maxDigits) {
        let s = value.toPrecision(maxDigits);
        if (s == "-Infinity") {
            s = "-inf";
        }
        else {
            // Only show maxDigits total digits including 0s:
            let digits = 0, n = 0;
            for (let c of s) {
                if (c >= '0' && c <= '9') {
                    digits++;
                    if (digits >= maxDigits) {
                        s = s.slice(0, n + 1);
                        break;
                    }
                }
                n++;
            }
        }
        return s;
    }
    function levelFormat(dB) {
        return `${withExactDigits(dB, 3)} dB`;
    }
    class MixerUI {
        constructor(mixer) {
            this.mixer = mixer;
        }
        trackFromDescendent(el) {
            let trackEl = el.closest("div.track");
            let trackName = trackEl.getAttribute("data-track");
            let track = this.mixer.track(trackName);
            return track;
        }
        faderInputHandler(e) {
            let el = e.target;
            let track = this.trackFromDescendent(el);
            let fader = el.value;
            let dB = fader_to_dB(fader);
            track.level.value = dB;
        }
        muteInputHandler(e) {
            let el = e.target;
            let track = this.trackFromDescendent(el);
            let mute = el.checked;
            track.mute.value = mute;
        }
        soloInputHandler(e) {
            let el = e.target;
            let track = this.trackFromDescendent(el);
            let solo = el.checked;
            track.solo.value = solo;
        }
        faderResetHandler(e) {
            let el = e.target;
            let track = this.trackFromDescendent(el);
            track.level.value = 0;
        }
        init(trackStrip, trackTemplate) {
            if (trackStrip == null) {
                trackStrip = document.querySelector(".webmixer .trackstrip");
                if (trackStrip == null) {
                    console.error("could not find trackstrip div element by selector '.webmixer .trackstrip'");
                    return;
                }
            }
            // Stamp template per each track:
            if (trackTemplate == null)
                trackTemplate = document.getElementById("trackTemplate");
            if (trackTemplate == null) {
                console.error("could not find track template element by selector '#trackTemplate'");
                return;
            }
            let faderInputHandler = this.faderInputHandler.bind(this);
            let faderResetHandler = this.faderResetHandler.bind(this);
            let muteInputHandler = this.muteInputHandler.bind(this);
            let soloInputHandler = this.soloInputHandler.bind(this);
            [...this.mixer.tracks, this.mixer.master].forEach(track => {
                // Clone template:
                const node = document.importNode(trackTemplate.content, true);
                // Set data-track attribute:
                const trackNode = node.querySelector("div.track");
                if (trackNode == null) {
                    console.error("could not find track node in template by selector 'div.track'");
                    return;
                }
                trackNode.setAttribute("data-track", track.name);
                // Set name label:
                const nameLabel = node.querySelector(".label span.name");
                if (nameLabel != null) {
                    nameLabel.innerText = track.name;
                }
                // Calculate EQ response:
                const eqCanvas = node.querySelector(".eq canvas.eq-response");
                if (eqCanvas != null) {
                    const n = 52 * 8;
                    function y(gain) {
                        return 312 - (gain_to_fader(gain) * 220.0);
                    }
                    function x(f) {
                        return Math.log(f / 20.0) / Math.log(1000) * (n - 1);
                    }
                    let eq = track.eq;
                    let resp = eq.responseCurve(n);
                    let ctx = eqCanvas.getContext("2d");
                    ctx.strokeStyle = '#555555';
                    ctx.lineWidth = 4;
                    ctx.beginPath();
                    ctx.moveTo(0, y(1));
                    ctx.lineTo(n, y(1));
                    ctx.stroke();
                    ctx.beginPath();
                    ctx.moveTo(x(20), 0);
                    ctx.lineTo(x(20), 312);
                    ctx.moveTo(x(200), 0);
                    ctx.lineTo(x(200), 312);
                    ctx.moveTo(x(2000), 0);
                    ctx.lineTo(x(2000), 312);
                    ctx.moveTo(x(20000), 0);
                    ctx.lineTo(x(20000), 312);
                    ctx.stroke();
                    ctx.beginPath();
                    ctx.moveTo(-1, y(resp.mag[0]));
                    for (let i = 1; i < n; i++) {
                        ctx.lineTo(i, y(resp.mag[i]));
                    }
                    ctx.lineWidth = 8;
                    ctx.strokeStyle = '#ffffff';
                    ctx.stroke();
                }
                // Set level label:
                const levelLabel = trackNode.querySelector(".label span.level");
                if (levelLabel != null) {
                    levelLabel.innerText = levelFormat(track.level.value);
                    // Click level label to reset to 0:
                    levelLabel.addEventListener("click", faderResetHandler);
                }
                // Bind fader events:
                const faderNode = trackNode.querySelector(".fader input[type=range]");
                if (faderNode != null) {
                    faderNode.min = '0.0';
                    faderNode.max = '1.0';
                    faderNode.valueAsNumber = dB_to_fader(track.level.value);
                    faderNode.addEventListener("dblclick", faderResetHandler);
                    faderNode.addEventListener("input", faderInputHandler);
                    if (levelLabel != null) {
                        track.level.addChangedEvent((value) => {
                            faderNode.valueAsNumber = dB_to_fader(value);
                            levelLabel.innerText = levelFormat(value);
                        });
                    }
                }
                const muteNode = trackNode.querySelector(".mute.button input[type=checkbox]");
                if (muteNode != null) {
                    muteNode.checked = track.mute.value;
                    muteNode.addEventListener("change", muteInputHandler);
                    track.mute.addChangedEvent((value) => {
                        muteNode.checked = value;
                    });
                }
                const soloNode = trackNode.querySelector(".solo.button input[type=checkbox]");
                if (soloNode != null) {
                    soloNode.checked = track.solo.value;
                    soloNode.addEventListener("change", soloInputHandler);
                    track.solo.addChangedEvent((value) => {
                        soloNode.checked = value;
                    });
                }
                trackStrip.appendChild(node);
            });
        }
    }
    exports.MixerUI = MixerUI;
}.apply(exports, __WEBPACK_AMD_DEFINE_ARRAY__),
				__WEBPACK_AMD_DEFINE_RESULT__ !== undefined && (module.exports = __WEBPACK_AMD_DEFINE_RESULT__));


/***/ }),
/* 1 */
/***/ (function(module, exports) {

module.exports = __WEBPACK_EXTERNAL_MODULE_1__;

/***/ })
/******/ ]);
});