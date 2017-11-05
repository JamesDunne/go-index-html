class Track {
    constructor(mixer, opts) {
        this.mixer = mixer;
        this.name = opts.name;
        this.channels = opts.channels || 1;
        this._pan = opts.pan || 0;
        this._level = opts.level || 0;
        this._level_offset = opts.level_offset || 0;
        this._mute = opts.mute || false;
        this._solo = opts.solo || false;
        this._soloMute = false;
    }

    createNodes(ac /*: AudioContext */) {
        // Create default nodes:
        this.soloMuteNode = ac.createGain();
        this.muteNode = ac.createGain();
        this.pannerNode = ac.createStereoPanner();
        this.gainNode = ac.createGain();

        // Connect nodes:
        this.soloMuteNode.connect(this.muteNode);
        this.muteNode.connect(this.pannerNode);
        this.pannerNode.connect(this.gainNode);

        // Set properties:
        this.setSoloMute();
        this.setMute();
        this.setPan();
        this.setLevel();
    }

    fireEvent(fn, value) {
        if (!(fn instanceof Function)) return;
        try {
            fn(value);
        } catch (e) {
            console.error(e);
        }
    }

    get inputNode() {
        return this.soloMuteNode;
    }

    get outputNode() {
        return this.gainNode;
    }

    get pan() {
        return this._pan;
    }
    set pan(value) {
        this._pan = value;
        this.fireEvent(this.panChanged, this._pan);
        this.setPan();
    }
    setPan() {
        if (!this.pannerNode) return;
        this.pannerNode.pan.value = this._pan;
        this.setLevel();
    }
    get panChanged() {
        return this._panChanged;
    }
    set panChanged(fn) {
        this._panChanged = fn;
    }

    get level() {
        return this._level;
    }
    set level(value) {
        this._level = value;
        this.fireEvent(this.levelChanged, this._level);
        this.setLevel();
    }
    setLevel() {
        if (!this.gainNode) return;
        let dB = this._level + this._level_offset;
        // Decrease apparent level depending on pan:
        dB += Math.abs(this._pan) * -6.0;
        this.gainNode.gain.value = Math.pow(10.0, dB / 20.0);
    }
    get levelChanged() {
        return this._levelChanged;
    }
    set levelChanged(fn) {
        this._levelChanged = fn;
    }

    get mute() {
        return this._mute;
    }
    set mute(value) {
        this._mute = value;
        this.fireEvent(this.muteChanged, this._mute);
        this.setMute();
    }
    setMute() {
        if (!this.muteNode) return;
        if (this._solo) {
            this.muteNode.gain.value = 1;
            return;
        }
        this.muteNode.gain.value = this._mute ? 0 : 1;
    }
    get muteChanged() {
        return this._muteChanged;
    }
    set muteChanged(fn) {
        this._muteChanged = fn;
    }

    get solo() {
        return this._solo;
    }
    set solo(value) {
        this._solo = value;
        this.fireEvent(this.soloChanged, this._solo);
        this.setSolo();
    }
    setSolo() {
        if (!this.mixer) return;
        this.mixer.applySolo();
    }
    get soloChanged() {
        return this._soloChanged;
    }
    set soloChanged(fn) {
        this._soloChanged = fn;
    }

    get soloMute() {
        return this._soloMute;
    }
    set soloMute(value) {
        this._soloMute = value;
        this.setSoloMute();
    }
    setSoloMute() {
        if (!this.soloMuteNode) return;
        if (this._solo) {
            this.muteNode.gain.value = 1;
        } else {
            this.setMute();
        }
        this.soloMuteNode.gain.value = this._soloMute ? 0 : 1;
    }
}
