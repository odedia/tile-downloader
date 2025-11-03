export namespace main {
	
	export class Release {
	    id: number;
	    version: string;
	    release_date: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new Release(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.version = source["version"];
	        this.release_date = source["release_date"];
	        this.description = source["description"];
	    }
	}
	export class Dependency {
	    release: Release;
	
	    static createFrom(source: any = {}) {
	        return new Dependency(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.release = this.convertValues(source["release"], Release);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DependencySpecifier {
	    id: number;
	    specifier: string;
	    // Go type: struct { ID int "json:\"id\""; Slug string "json:\"slug\""; Name string "json:\"name\"" }
	    product: any;
	
	    static createFrom(source: any = {}) {
	        return new DependencySpecifier(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.specifier = source["specifier"];
	        this.product = this.convertValues(source["product"], Object);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EULA {
	    id: number;
	    slug: string;
	    name: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new EULA(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.slug = source["slug"];
	        this.name = source["name"];
	        this.content = source["content"];
	    }
	}
	export class Product {
	    id: number;
	    slug: string;
	    name: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new Product(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.slug = source["slug"];
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}
	export class ProductFile {
	    id: number;
	    name: string;
	    aws_object_key: string;
	    file_type: string;
	    file_version: string;
	    md5: string;
	    sha256: string;
	
	    static createFrom(source: any = {}) {
	        return new ProductFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.aws_object_key = source["aws_object_key"];
	        this.file_type = source["file_type"];
	        this.file_version = source["file_version"];
	        this.md5 = source["md5"];
	        this.sha256 = source["sha256"];
	    }
	}

}

